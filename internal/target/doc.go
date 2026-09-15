// Package target turns what the user asked to mount into a mount.Spec.
//
// It is the layer between the command line and sshfs: it decides which of a
// favourite, the ssh config and the command line supplies each part of a
// mount, and in what order their options are applied.
package target
