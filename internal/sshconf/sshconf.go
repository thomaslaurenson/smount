// Package sshconf discovers host aliases from OpenSSH client configuration
// files and resolves the effective settings for one of them.
//
// Discovery and resolution are deliberately separate jobs done by separate
// means. Alias names can only be read out of the config files, because ssh
// itself will happily resolve a name that appears nowhere and has no way to
// list what it knows. Everything else about a host is read back from
// "ssh -G", because reimplementing Match blocks, CanonicalizeHostname and
// per-option first-wins precedence would be a slow way of arriving at a worse
// answer than ssh already has.
package sshconf

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// resolveWorkerCap bounds how many ssh processes run at once when resolving a
// whole config, however many cores are available.
//
// Each one is short lived, so this is about not forking a config's worth of
// processes at the same instant rather than about load. Past this the wall
// clock stops improving, and a config whose Match exec does a name lookup gets
// worse rather than better: the lookups land on the resolver as one burst and
// start timing out in numbers they would not have alone.
const resolveWorkerCap = 32

// resolveWorkerFloor keeps a small machine from resolving a large config nearly
// one host at a time. An "ssh -G" spends most of its life starting up and
// parsing, not computing, so more of them than cores is the point.
const resolveWorkerFloor = 8

// resolveWorkers returns how many ssh processes to run at once for n aliases.
//
// The work is one short process per alias, so the wall clock is however long
// they take divided by however many run at once. Sizing the pool to the machine
// rather than fixing it is what keeps a large config from being resolved a
// handful at a time.
func resolveWorkers(n int) int {
	workers := runtime.NumCPU() * 4
	if workers < resolveWorkerFloor {
		workers = resolveWorkerFloor
	}
	if workers > resolveWorkerCap {
		workers = resolveWorkerCap
	}
	if n < workers {
		workers = n
	}
	return workers
}

// ResolveTimeout bounds one "ssh -G".
//
// Applying a config is local work that takes a few tens of milliseconds, but a
// config can make it reach the network: a Match exec block runs its command
// every time ssh reads the config, and CanonicalizeHostname adds a lookup of
// its own. A name that does not resolve then stalls for the resolver's own
// timeout, which is five seconds on a stock glibc, and listing a whole config
// pays that for every such host.
//
// A host that does not answer in this long is reported as unresolved, which the
// listing already renders as "-". These settings are a decoration on a list of
// names: sshfs runs its own ssh and resolves the host for itself regardless, so
// a missing line here costs nothing but the display.
const ResolveTimeout = 2 * time.Second

// resolveWaitDelay is how long a killed ssh is given to release its pipes
// before they are closed for it, bounding the overshoot past ResolveTimeout.
const resolveWaitDelay = 250 * time.Millisecond

// maxIncludeDepth matches the nesting limit OpenSSH enforces on Include.
const maxIncludeDepth = 16

// Host holds the settings ssh resolved for an alias.
type Host struct {
	Name     string
	HostName string
	User     string
	Port     string
	Identity string
}

// Addr returns the user@host:port form of the resolved settings, for display.
func (h *Host) Addr() string {
	addr := h.HostName
	if h.User != "" {
		addr = h.User + "@" + addr
	}
	if h.Port != "" && h.Port != defaultPort {
		addr += ":" + h.Port
	}
	return addr
}

// defaultPort is the port ssh uses when a config names none, and so the one
// worth saying nothing about.
const defaultPort = "22"

// defaultsProbe is the alias Defaults asks about.
//
// It ends in .invalid, which RFC 2606 reserves and no real host can use, so a
// config naming it is close to impossible. A "Host *" block still matches it,
// which is the point: the answer has to include whatever applies to every
// host, since that is exactly what is not worth printing per host.
const defaultsProbe = "smount-defaults.invalid"

// Defaults returns the settings ssh applies to a host with nothing configured
// for it.
//
// This is what separates a setting worth printing from one the whole config
// shares. ssh reports a user and a port for every host whether the config
// names them or not, so with nothing to compare against, a listing repeats the
// same two values on every row.
//
// Only User and Port are meaningful in the result. The HostName it reports is
// the probe itself and says nothing about any real host.
func Defaults(ctx context.Context) (*Host, error) {
	return Resolve(ctx, defaultsProbe)
}

// Describe renders what ssh resolved that the alias does not already say,
// returning the empty string when it says nothing new.
//
// defaults comes from Defaults and is what ssh applies to a host configured
// nowhere. A user or port matching it is left out, because ssh reports both
// for every host and a value the whole config shares tells the reader nothing
// about this one. A nil defaults leaves the user in, which is the right way to
// fail when the baseline could not be resolved: showing too much is recoverable
// and hiding a real setting is not.
//
// A host that adds only a user or a port still shows its name, because
// "tlau083@" alone reads as an unfinished address.
func (h *Host) Describe(defaults *Host) string {
	user := h.User
	if defaults != nil && user == defaults.User {
		user = ""
	}
	hostName := h.HostName
	if hostName == h.Name {
		hostName = ""
	}
	port := h.Port
	if port == defaultPort || (defaults != nil && port == defaults.Port) {
		port = ""
	}
	if user == "" && hostName == "" && port == "" {
		return ""
	}

	if hostName == "" {
		hostName = h.Name
	}
	out := hostName
	if user != "" {
		out = user + "@" + out
	}
	if port != "" {
		out += ":" + port
	}
	return out
}

// ErrInvalidAlias is returned for an alias ssh would misread as an option.
//
// A leading "-" makes ssh read the alias as a command line option, not a
// host.
var ErrInvalidAlias = errors.New("host alias must not begin with '-'")

// Aliases returns every concrete host alias reachable from path.
//
// Include directives are followed, and the result is sorted and deduplicated.
//
// Wildcard and negated patterns are omitted. "Host *" sets defaults for every
// connection rather than naming somewhere to connect to, so offering it as a
// mount target would be offering something that cannot be mounted.
func Aliases(home, path string) ([]string, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("reading ssh config: %w", err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	names := make(map[string]struct{})
	visited := make(map[string]struct{})
	if err := collect(abs, filepath.Dir(abs), home, names, visited, 0); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	slices.Sort(out)
	return out, nil
}

// HasAlias reports whether target names a concrete host alias reachable from
// path.
//
// This is what tells a typo from a host that is simply configured elsewhere.
// "ssh -G" echoes an unknown name straight back as its own hostname, so it can
// never answer the question.
func HasAlias(home, path, target string) (bool, error) {
	aliases, err := Aliases(home, path)
	if err != nil {
		return false, err
	}
	return slices.Contains(aliases, aliasName(target)), nil
}

// aliasName strips a "user@" prefix from target, leaving the name ssh would
// match a Host pattern against.
func aliasName(target string) string {
	if _, after, found := strings.Cut(target, "@"); found {
		return after
	}
	return target
}

// collect reads one config file into names, recursing through its includes.
//
// root is the directory a relative Include resolves against, which is fixed for
// the whole traversal rather than following the file being read.
//
// A missing or unreadable file is not an error below the top level. ssh treats
// an Include that matches nothing as a no-op, and a config that names an
// optional work-only fragment is a normal thing to carry between machines.
func collect(path, root, home string, names, visited map[string]struct{}, depth int) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("ssh config includes nested more than %d deep at %s", maxIncludeDepth, path)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, seen := visited[abs]; seen {
		return nil
	}
	visited[abs] = struct{}{}

	file, err := os.Open(abs)
	if err != nil {
		if depth == 0 {
			return fmt.Errorf("reading ssh config: %w", err)
		}
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		tokens := tokenise(scanner.Text())
		if len(tokens) < 2 {
			continue
		}
		switch strings.ToLower(tokens[0]) {
		case "host":
			for _, pattern := range tokens[1:] {
				if isConcrete(pattern) {
					names[pattern] = struct{}{}
				}
			}
		case "include":
			for _, pattern := range tokens[1:] {
				for _, inc := range includePaths(pattern, root, home) {
					if err := collect(inc, root, home, names, visited, depth+1); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", abs, err)
	}
	return nil
}

// includePaths expands one Include argument into the files it matches, with a
// relative pattern resolved against root.
//
// ssh resolves a relative Include against ~/.ssh for a user config, whichever
// file the directive appears in. Resolving against the directory of the file
// holding it instead looks equivalent and is not: an "Include hosts" inside
// ~/.ssh/config.d/work means ~/.ssh/hosts to ssh, so following the current file
// both misses hosts ssh can reach and offers hosts it cannot.
//
// root is therefore the directory of the top level config for the whole
// traversal. That is the same directory as ~/.ssh whenever ssh_config points at
// ~/.ssh/config, and it keeps a config kept somewhere else self contained. The
// one case it still differs from ssh is ssh_config pointing at a fragment, say
// ~/.ssh/config.d/work, where ssh would resolve a relative Include inside it
// against ~/.ssh and this resolves against ~/.ssh/config.d.
func includePaths(pattern, root, home string) []string {
	expanded := expandUser(pattern, home)
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(root, expanded)
	}
	matches, err := filepath.Glob(expanded)
	if err != nil {
		// The only error Glob reports is a malformed pattern, which ssh also
		// ignores rather than refusing to start.
		return nil
	}
	return matches
}

// isConcrete reports whether a Host pattern names a single host that could be
// connected to, rather than matching a set of them.
func isConcrete(pattern string) bool {
	if pattern == "" || strings.HasPrefix(pattern, "!") {
		return false
	}
	return !strings.ContainsAny(pattern, "*?")
}

// tokenise splits one ssh_config line into a keyword and its arguments.
//
// Arguments may be double quoted, and ssh accepts either whitespace or a single
// equals sign between the keyword and the first argument, so "Host=web" and
// "Host web" are the same directive. An equals sign anywhere later is an
// ordinary character, which matters for values such as ProxyCommand.
//
// A "#" ends the line only where a new token would begin. That is narrower than
// stripping everything from the first "#", and it is what ssh does: "Host web01
// # my box" names one host, while "Host web01#1" and "Host "#tag"" both name a
// host whose name contains the hash.
func tokenise(line string) []string {
	line = strings.TrimLeft(line, " \t")
	if line == "" {
		return nil
	}

	var (
		tokens  []string
		current strings.Builder
		quoted  bool
		started bool
	)
	flush := func() {
		if started {
			tokens = append(tokens, current.String())
			current.Reset()
			started = false
		}
	}

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			quoted = !quoted
			started = true
		case quoted:
			current.WriteByte(c)
			started = true
		case c == '#' && !started:
			return tokens
		case c == ' ' || c == '\t':
			flush()
		case c == '=' && len(tokens) == 0 && started:
			flush()
		case c == '=' && len(tokens) == 1 && !started:
			// Separator in the "Host = web" form, already past the keyword.
		default:
			current.WriteByte(c)
			started = true
		}
	}
	flush()
	return tokens
}

// expandUser resolves a leading ~ against home, taking it as a plain string so
// that config parsing stays free of smount's own path conventions.
func expandUser(path, home string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// Resolve reports the settings ssh would use for an alias.
//
// "ssh -G" performs no network activity: it applies the config files and prints
// the result, so this is cheap enough to call before every mount.
//
// ssh is deliberately left to find its own configuration rather than being
// pointed at the file the alias was discovered in. Passing -F changes how a
// relative Include resolves and stops it finding the included file at all, and
// it would only ever line up the displayed settings anyway: sshfs runs its own
// ssh, which reads the default configuration no matter what smount was told to
// read.
func Resolve(ctx context.Context, alias string) (*Host, error) {
	if strings.HasPrefix(alias, "-") {
		return nil, ErrInvalidAlias
	}

	ctx, cancel := context.WithTimeout(ctx, ResolveTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh", "-G", alias)
	// Killing ssh is not on its own enough to end the read. A Match exec block
	// runs its command through a shell, and that shell's own children inherit
	// the pipe Output reads from, so an orphaned lookup holds the pipe open and
	// Wait sits on it until the grandchild finishes by itself: exactly the wait
	// the timeout above exists to cut short. WaitDelay closes the pipes shortly
	// after the kill, which is what makes that timeout the bound it looks like.
	cmd.WaitDelay = resolveWaitDelay
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(bytes.TrimSpace(exit.Stderr)) > 0 {
			return nil, fmt.Errorf("ssh -G %s: %s", alias, bytes.TrimSpace(exit.Stderr))
		}
		return nil, fmt.Errorf("ssh -G %s: %w", alias, err)
	}
	return parseResolved(alias, out), nil
}

// ResolveAll resolves every alias concurrently, keyed by alias.
//
// Aliases that fail are omitted rather than reported. This exists to decorate a
// list of hosts with where each one actually points, and a host whose config
// ssh will not read is still a host worth showing by name.
func ResolveAll(ctx context.Context, aliases []string) map[string]*Host {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]*Host, len(aliases))
		work    = make(chan string)
	)

	workers := resolveWorkers(len(aliases))
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for alias := range work {
				host, err := Resolve(ctx, alias)
				if err != nil {
					continue
				}
				mu.Lock()
				results[alias] = host
				mu.Unlock()
			}
		}()
	}
	for _, alias := range aliases {
		select {
		case <-ctx.Done():
			// Stop handing out work; the workers drain what they have and the
			// map is returned with whatever was resolved before the interrupt.
			close(work)
			wg.Wait()
			return results
		case work <- alias:
		}
	}
	close(work)
	wg.Wait()

	return results
}

// parseResolved reads the "keyword value" lines that ssh -G prints.
func parseResolved(alias string, out []byte) *Host {
	h := &Host{Name: alias}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		key, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		if !found {
			continue
		}
		switch strings.ToLower(key) {
		case "hostname":
			h.HostName = value
		case "user":
			h.User = value
		case "port":
			h.Port = value
		case "identityfile":
			// ssh prints every candidate identity in precedence order, so the
			// first is the one it would offer first.
			if h.Identity == "" {
				h.Identity = value
			}
		}
	}
	if h.HostName == "" {
		h.HostName = alias
	}
	return h
}
