package sshconf

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTokenise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "keyword and argument", input: "Host web01", want: []string{"Host", "web01"}},
		{name: "leading whitespace", input: "    Host web01", want: []string{"Host", "web01"}},
		{name: "tab separated", input: "\tHost\tweb01", want: []string{"Host", "web01"}},
		{name: "several arguments", input: "Host a b c", want: []string{"Host", "a", "b", "c"}},
		{name: "equals separator", input: "Host=web01", want: []string{"Host", "web01"}},
		{name: "spaced equals separator", input: "Host = web01", want: []string{"Host", "web01"}},
		{name: "equals inside argument", input: "SetEnv FOO=bar", want: []string{"SetEnv", "FOO=bar"}},
		{name: "quoted argument", input: `Include "my configs/*"`, want: []string{"Include", "my configs/*"}},
		{name: "comment", input: "# Host web01", want: nil},
		{name: "indented comment", input: "   # Host web01", want: nil},
		{name: "blank", input: "   ", want: nil},
		{name: "keyword only", input: "Host", want: []string{"Host"}},
		{name: "trailing comment", input: "Host web01 # my box", want: []string{"Host", "web01"}},
		{name: "tab before trailing comment", input: "Host web01\t#my box", want: []string{"Host", "web01"}},
		{name: "comment after the keyword", input: "Host # web01", want: []string{"Host"}},
		{name: "hash inside an argument", input: "Host web01#1", want: []string{"Host", "web01#1"}},
		{name: "hash inside quotes", input: `Host "#tag"`, want: []string{"Host", "#tag"}},
		{name: "hash after a closing quote", input: `Host "a"#b`, want: []string{"Host", "a#b"}},
		{name: "comment after an equals separator", input: "Host = # web01", want: []string{"Host"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tokenise(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tokenise(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsConcrete(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{name: "plain name", pattern: "web01", want: true},
		{name: "dotted name", pattern: "web01.example.com", want: true},
		{name: "star", pattern: "*", want: false},
		{name: "embedded star", pattern: "web*", want: false},
		{name: "question mark", pattern: "web0?", want: false},
		{name: "negated", pattern: "!web01", want: false},
		{name: "empty", pattern: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isConcrete(tc.pattern); got != tc.want {
				t.Errorf("isConcrete(%q) = %v, want %v", tc.pattern, got, tc.want)
			}
		})
	}
}

func TestAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name:  "simple hosts",
			files: map[string]string{"config": "Host web01\nHost web02\n"},
			want:  []string{"web01", "web02"},
		},
		{
			name:  "indented host is still a host",
			files: map[string]string{"config": "Host web01\n    Host web02\n"},
			want:  []string{"web01", "web02"},
		},
		{
			name:  "several names on one line",
			files: map[string]string{"config": "Host alpha beta gamma\n"},
			want:  []string{"alpha", "beta", "gamma"},
		},
		{
			name:  "wildcards and negations omitted",
			files: map[string]string{"config": "Host *\nHost web01\nHost !bad\nHost web?\n"},
			want:  []string{"web01"},
		},
		{
			name:  "trailing comments are not host names",
			files: map[string]string{"config": "Host web01 # my production box\nHost web02\t#staging\n"},
			want:  []string{"web01", "web02"},
		},
		{
			name: "include with a glob",
			files: map[string]string{
				"config":         "Host top\nInclude config.d/*\n",
				"config.d/work":  "Host work01\n",
				"config.d/extra": "Host extra01\n",
			},
			want: []string{"extra01", "top", "work01"},
		},
		{
			// ssh resolves a relative Include against the directory of the top
			// level config whichever file it appears in, so "Include two" inside
			// config.d/one means ./two and never config.d/two.
			name: "nested relative include resolves against the top level config",
			files: map[string]string{
				"config":        "Include config.d/one\n",
				"config.d/one":  "Host one\nInclude two\n",
				"two":           "Host two\n",
				"config.d/two":  "Host beside-the-includer\n",
				"config.d/none": "Host unreferenced\n",
			},
			want: []string{"one", "two"},
		},
		{
			name: "duplicate host across files is listed once",
			files: map[string]string{
				"config":       "Host shared\nInclude config.d/a\n",
				"config.d/a":   "Host shared\n",
				"config.d/b.x": "",
			},
			want: []string{"shared"},
		},
		{
			name:  "include matching nothing is ignored",
			files: map[string]string{"config": "Host web01\nInclude config.d/*\n"},
			want:  []string{"web01"},
		},
		{
			name: "include cycle terminates",
			files: map[string]string{
				"config":     "Host one\nInclude config.d/a\n",
				"config.d/a": "Host two\nInclude ../config\n",
			},
			want: []string{"one", "two"},
		},
		{
			name:  "comments ignored",
			files: map[string]string{"config": "# Host commented\nHost real\n"},
			want:  []string{"real"},
		},
		{
			name:  "equals form",
			files: map[string]string{"config": "Host=web01\n"},
			want:  []string{"web01"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := writeTree(t, tc.files)

			got, err := Aliases(dir, filepath.Join(dir, "config"))
			if err != nil {
				t.Fatalf("Aliases() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Aliases() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAliasesMissingConfig(t *testing.T) {
	t.Parallel()
	if _, err := Aliases("", filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("Aliases() on a missing config = nil error, want an error")
	}
}

func TestParseResolved(t *testing.T) {
	t.Parallel()
	out := []byte("host web01\n" +
		"hostname 10.0.0.4\n" +
		"user deploy\n" +
		"port 2222\n" +
		"identityfile ~/.ssh/id_ed25519\n" +
		"forwardagent no\n")

	got := parseResolved("web01", out)
	want := &Host{
		Name:     "web01",
		HostName: "10.0.0.4",
		User:     "deploy",
		Port:     "2222",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseResolved() = %+v, want %+v", got, want)
	}
}

func TestParseResolvedDefaultsHostName(t *testing.T) {
	t.Parallel()
	got := parseResolved("web01", []byte("user deploy\n"))
	if got.HostName != "web01" {
		t.Errorf("HostName = %q, want %q", got.HostName, "web01")
	}
}

func TestResolveRejectsFlagLikeAlias(t *testing.T) {
	t.Parallel()
	if _, err := Resolve(t.Context(), "-oProxyCommand=touch /tmp/pwned"); err != ErrInvalidAlias {
		t.Errorf("Resolve() error = %v, want %v", err, ErrInvalidAlias)
	}
}

func TestHasAlias(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{
		"config": "Host web01\nHost db-prod\nHost *\n",
	})
	path := filepath.Join(dir, "config")

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "known alias", target: "web01", want: true},
		{name: "known alias with a user prefix", target: "root@web01", want: true},
		{name: "unknown alias", target: "web02", want: false},
		{name: "unknown alias with a user prefix", target: "root@web02", want: false},
		{name: "a wildcard pattern is not an alias", target: "*", want: false},
		{name: "the user part is not matched on its own", target: "web01@db-prod", want: true},
		{name: "empty", target: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := HasAlias(dir, path, tc.target)
			if err != nil {
				t.Fatalf("HasAlias(%q) error = %v", tc.target, err)
			}
			if got != tc.want {
				t.Errorf("HasAlias(%q) = %v, want %v", tc.target, got, tc.want)
			}
		})
	}
}

func TestHasAliasReportsAMissingConfig(t *testing.T) {
	t.Parallel()
	if _, err := HasAlias("", filepath.Join(t.TempDir(), "absent"), "web01"); err == nil {
		t.Error("HasAlias() on a missing config = nil error, want an error")
	}
}

func TestAliasName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "bare host", input: "web01", want: "web01"},
		{name: "user prefix stripped", input: "root@web01", want: "web01"},
		{name: "only the first at sign separates", input: "a@b@web01", want: "b@web01"},
		{name: "empty user", input: "@web01", want: "web01"},
		{name: "empty", input: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := aliasName(tc.input); got != tc.want {
				t.Errorf("aliasName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// writeTree creates the given files, relative to a fresh temporary directory,
// and returns that directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return dir
}

func TestResolveWorkers(t *testing.T) {
	cap := resolveWorkers(1 << 20)
	if cap > resolveWorkerCap {
		t.Errorf("resolveWorkers(large) = %d, want at most %d", cap, resolveWorkerCap)
	}
	if cap < resolveWorkerFloor {
		t.Errorf("resolveWorkers(large) = %d, want at least %d", cap, resolveWorkerFloor)
	}

	// Never more workers than there is work for them to do, so a one host
	// config starts one process rather than a machine's worth of them.
	for _, n := range []int{0, 1, 2, 5} {
		if got := resolveWorkers(n); got != n {
			t.Errorf("resolveWorkers(%d) = %d, want %d", n, got, n)
		}
	}

	// More than one process per core: an "ssh -G" is mostly startup and
	// parsing, so the cores are not what is being shared out.
	if runtime.NumCPU()*4 <= resolveWorkerCap && cap <= runtime.NumCPU() {
		t.Errorf("resolveWorkers(large) = %d, want more than NumCPU (%d)", cap, runtime.NumCPU())
	}
}

// stubSSH puts a fake ssh on PATH that sleeps for the given duration, so the
// timeout can be exercised without running the real thing or touching a
// network.
func stubSSH(t *testing.T, sleep string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub relies on a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nsleep " + sleep + "\necho 'hostname stub.example'\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Prepended rather than replacing PATH, so the stub shadows ssh while the
	// script can still find the shell utilities it runs.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A host that never answers must not hold the whole listing up. A config whose
// Match exec runs a name lookup reaches the resolver on every alias, and a name
// that does not resolve stalls there for seconds.
func TestResolveGivesUpOnAStalledSSH(t *testing.T) {
	stubSSH(t, "30")

	start := time.Now()
	_, err := Resolve(t.Context(), "wedged")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Resolve() = nil error, want a timeout")
	}
	if elapsed > ResolveTimeout*2 {
		t.Errorf("Resolve() took %v, want it abandoned near %v", elapsed, ResolveTimeout)
	}
}

// The bound is on one host, so a whole config of stalled hosts still finishes.
func TestResolveAllFinishesWhenEveryHostStalls(t *testing.T) {
	stubSSH(t, "30")

	aliases := make([]string, 20)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("wedged%02d", i)
	}

	start := time.Now()
	got, defaults := ResolveAll(t.Context(), aliases)
	elapsed := time.Since(start)

	if len(got) != 0 {
		t.Errorf("ResolveAll() resolved %d hosts, want none", len(got))
	}
	if defaults != nil {
		t.Errorf("ResolveAll() baseline = %+v, want nil when the probe stalls too", defaults)
	}
	if elapsed > ResolveTimeout*3 {
		t.Errorf("ResolveAll() took %v, want it bounded near %v", elapsed, ResolveTimeout)
	}
}

// TestResolveAllRunsTheBaselineBesideTheHosts is the guard for the baseline
// overlapping rather than queueing. Run in turn its wait is added to a listing
// that has already finished, which doubles the wall clock of a config whose
// hosts are slow to resolve.
func TestResolveAllRunsTheBaselineBesideTheHosts(t *testing.T) {
	stubSSH(t, "1")

	start := time.Now()
	got, defaults := ResolveAll(t.Context(), []string{"web01", "db-prod"})
	elapsed := time.Since(start)

	if len(got) != 2 {
		t.Errorf("ResolveAll() resolved %d hosts, want 2", len(got))
	}
	if defaults == nil {
		t.Fatal("ResolveAll() baseline = nil, want the probe resolved")
	}
	if defaults.HostName != "stub.example" {
		t.Errorf("baseline HostName = %q, want %q", defaults.HostName, "stub.example")
	}
	// Two seconds is the sequential cost of one second of hosts followed by
	// one second of baseline. Anything near it means they did not overlap.
	if elapsed > 1900*time.Millisecond {
		t.Errorf("ResolveAll() took %v, want the baseline resolved alongside the hosts", elapsed)
	}
}

// A host that answers inside the bound is still resolved normally.
func TestResolveSucceedsInsideTheTimeout(t *testing.T) {
	stubSSH(t, "0")

	host, err := Resolve(t.Context(), "quick")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if host.HostName != "stub.example" {
		t.Errorf("HostName = %q, want %q", host.HostName, "stub.example")
	}
}

func TestHostDescribe(t *testing.T) {
	t.Parallel()
	// What ssh reports for a host the config says nothing about: the local
	// account and the standard port.
	plain := &Host{Name: defaultsProbe, User: "thomas", Port: "22"}
	// A config with a "Host *" block, where the shared user and port are the
	// uninteresting ones and the local account never appears.
	global := &Host{Name: defaultsProbe, User: "tlau083", Port: "2202"}

	tests := []struct {
		name     string
		host     Host
		defaults *Host
		want     string
	}{
		{
			name:     "an alias that resolves to itself says nothing",
			host:     Host{Name: "web01", HostName: "web01", User: "thomas", Port: "22"},
			defaults: plain,
			want:     "",
		},
		{
			name:     "a different hostname is worth showing",
			host:     Host{Name: "nesi", HostName: "login.mahuika.nesi.org.nz", User: "thomas", Port: "22"},
			defaults: plain,
			want:     "login.mahuika.nesi.org.nz",
		},
		{
			// The name comes back with it, because a bare "tlau083@" reads as
			// an address someone forgot to finish.
			name:     "a different user keeps the name beside it",
			host:     Host{Name: "compute-01", HostName: "compute-01", User: "tlau083", Port: "22"},
			defaults: plain,
			want:     "tlau083@compute-01",
		},
		{
			name:     "a non-default port keeps the name beside it",
			host:     Host{Name: "web01", HostName: "web01", User: "thomas", Port: "2222"},
			defaults: plain,
			want:     "web01:2222",
		},
		{
			name:     "everything different at once",
			host:     Host{Name: "web01", HostName: "10.0.0.15", User: "deploy", Port: "2222"},
			defaults: plain,
			want:     "deploy@10.0.0.15:2222",
		},
		{
			// The case the local account could not answer: with "Host *" setting
			// a user, every host resolves to it, and repeating it on every row
			// says nothing about any of them.
			name:     "a user shared by the whole config is not worth showing",
			host:     Host{Name: "web01", HostName: "web01", User: "tlau083", Port: "2202"},
			defaults: global,
			want:     "",
		},
		{
			name:     "a host overriding the shared user still shows it",
			host:     Host{Name: "web01", HostName: "web01", User: "deploy", Port: "2202"},
			defaults: global,
			want:     "deploy@web01",
		},
		{
			// Failing to resolve the baseline leaves the user in. Showing too
			// much can be read past; hiding a real setting cannot.
			name:     "no baseline shows the user",
			host:     Host{Name: "web01", HostName: "web01", User: "thomas", Port: "22"},
			defaults: nil,
			want:     "thomas@web01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.host.Describe(tc.defaults); got != tc.want {
				t.Errorf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDefaultsProbeMatchesNothingReal guards the probe alias. A name a config
// could plausibly define would return that host's settings as the baseline and
// suppress them everywhere.
func TestDefaultsProbeMatchesNothingReal(t *testing.T) {
	t.Parallel()
	if !strings.HasSuffix(defaultsProbe, ".invalid") {
		t.Errorf("defaultsProbe = %q, want a name under the reserved .invalid", defaultsProbe)
	}
}
