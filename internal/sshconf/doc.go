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
