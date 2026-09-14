# Changelog

## 0.5.0 - 2026-09-14

### Changed

- Bring the release and prerelease workflows in line with the Go spec

## 0.4.0 - 2026-09-10

### Added

- Add a color flag, honouring NO_COLOR and whether the stream is a terminal
- Add a short flag to ls for printing bare mount names
- Write the default config file on first run

### Changed

- Fit the host and mount listings to the terminal, clipping long names in the middle
- Leave out cells and columns that only repeat what another column already says
- Cut the mount summary to the lines that say something new
- Hold the interactive menu columns still while scrolling
- Search and highlight the menu detail column when filtering
- Use one marker vocabulary for every message, replacing the check specific pair
- Rename the hosts quiet flag to short, matching the new ls flag

### Removed

- Remove the identity column from the host listing

## 0.3.0 - 2026-09-08

### Removed

- Remove the fav subcommand for listing, adding and deleting favourites

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
