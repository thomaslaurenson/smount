package mount

import (
	"fmt"
	"regexp"
	"strings"
)

// diagnosis maps a line ssh logs on a failed connection to an explanation of
// it. The explanation is a template, so it can name what the line names: the
// host, the port or the file.
type diagnosis struct {
	match  *regexp.Regexp
	reason string
}

// diagnoses is checked in order and the first match wins, so a specific
// explanation comes before a general one that the same failure also logs. A
// security key that is not plugged in, for one, logs the signing failure and
// then the server's refusal, and only the first of those says what to do.
var diagnoses = []diagnosis{
	{
		regexp.MustCompile(`signing failed.*device not found`),
		"the security key is not plugged in, insert it and try again",
	},
	{
		regexp.MustCompile(`signing failed.*incorrect passphrase`),
		"the passphrase or PIN for the key was wrong",
	},
	{
		regexp.MustCompile(`agent refused operation`),
		"the ssh agent refused to use the key, check a security key is plugged in and touched in time",
	},
	{
		regexp.MustCompile(`Permissions \d+ for '([^']+)' are too open`),
		"ssh ignored ${1} because others can read it, run 'chmod 600 ${1}'",
	},
	{
		regexp.MustCompile(`Bad owner or permissions on (\S+)`),
		"ssh will not read ${1} while others can write to it, run 'chmod 600 ${1}'",
	},
	{
		regexp.MustCompile(`Too many authentication failures`),
		"the server gave up after ssh offered too many keys, set IdentitiesOnly and an IdentityFile for this host",
	},
	{
		regexp.MustCompile(`Permission denied \(([^)]*)\)`),
		"the server rejected the login (it accepts ${1}), 'ssh -v' shows which keys were offered",
	},
	{
		regexp.MustCompile(`Offending \S+ key in (\S+)`),
		"the host key has changed since ${1} recorded it, find out why before replacing it",
	},
	{
		regexp.MustCompile(`REMOTE HOST IDENTIFICATION HAS CHANGED`),
		"the host key has changed since it was last seen, find out why before replacing it",
	},
	{
		regexp.MustCompile(`Host key verification failed`),
		"the host key is not trusted yet, connect with ssh once to check and accept it",
	},
	{
		regexp.MustCompile(`Could not resolve hostname ([^:]+)`),
		"${1} does not resolve, check the name, its HostName, DNS or the VPN",
	},
	{
		regexp.MustCompile(`connect to host (\S+) port (\d+): Connection refused`),
		"${1} refused the connection on port ${2}, check the port and that sshd is running",
	},
	{
		regexp.MustCompile(`connect to host (\S+) port \d+: (?:Connection timed out|Operation timed out|No route to host|Network is unreachable)`),
		"${1} did not answer, check the network or the VPN",
	},
	{
		regexp.MustCompile(`Unable to negotiate with .*?: (no matching .*? found)`),
		"ssh and the server could not agree, ${1}",
	},
	{
		regexp.MustCompile(`(?i)subsystem.*failed`),
		"the server does not offer sftp, which sshfs needs",
	},
	// Last, because it is what every connection the server cuts short ends
	// with, whatever cut it. A jump host that cannot reach the target lands
	// here too: the jump host's own ssh is not the one writing this log.
	{
		regexp.MustCompile(`kex_exchange_identification|Connection closed by`),
		"the server closed the connection before login, often rate limiting such as fail2ban, or a jump host that could not get through",
	},
}

// unexplainedLines is how much of the log a failure nothing explains shows,
// counted from the end, which is where ssh puts the reason it gave up.
const unexplainedLines = 5

// diagnose explains a failed connection from what ssh logged, returning the
// explanation and the line that gave it away.
func diagnose(logged []string) (reason, evidence string, ok bool) {
	for _, d := range diagnoses {
		for _, line := range logged {
			m := d.match.FindStringSubmatchIndex(line)
			if m == nil {
				continue
			}
			reason := string(d.match.ExpandString(nil, d.reason, line, m))
			// The host key warnings are drawn as a box of @ characters.
			return reason, strings.Trim(line, "@ "), true
		}
	}
	return "", "", false
}

// failure builds the error for a mount sshfs refused.
//
// What sshfs says is always the same and never the reason, so the error is
// built from ssh's log: an explanation where the log matches something known,
// the end of the log where it does not, and the old advice to run ssh by hand
// where there is no log at all.
func failure(s Spec, err error, logged []string) error {
	if reason, evidence, ok := diagnose(logged); ok {
		return fmt.Errorf("mounting %s: %s%s", s.Describe(), reason, indent([]string{evidence}))
	}
	if len(logged) > 0 {
		return fmt.Errorf("mounting %s: %w, ssh logged:%s", s.Describe(), err, indent(logged[max(0, len(logged)-unexplainedLines):]))
	}
	// sshfs reports a failed ssh as a pipe error, so "connection reset by
	// peer" is what a rejected key looks like here. ssh says why, and is the
	// fastest way to find out.
	return fmt.Errorf("mounting %s: %w, try 'ssh %s' to see why", s.Describe(), err, s.Host)
}

// indent renders lines as a block under an error message, lined up with the
// text after the marker it is printed behind.
func indent(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("\n    ")
		b.WriteString(line)
	}
	return b.String()
}
