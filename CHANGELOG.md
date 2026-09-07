# Changelog

## 0.2.1 - 2026-09-08

### Changed

- Speed up host listing and the interactive host picker

### Fixed

- Keep interactive menus from taking keystrokes meant for later prompts and for ssh
- Stop a host that will not resolve from holding up the host listing

## 0.2.0 - 2026-08-31

### Fixed

- Stop cleanly on Ctrl-C, restoring the terminal and stopping the sshfs it started
- Report an unresolvable home directory instead of mounting under a literal ~ path

## 0.1.0 - 2026-08-21

### Added

- Initial upload
