# ADR-017: Opt-In Prerequisite Provisioning

**Status:** Accepted
**Date:** 2026-10-17

## Context

ADR-016 promised one command that produces a working install. In practice the
first thing a new machine fails on is the thing Scopra cannot supply itself:
rootless Podman. The preflight refuses to continue without it, correctly, and
then prints "install Podman 4.0 or later" — which is exactly the manual step the
one-command install was supposed to remove. The user is left reading a fix line
and typing it into another terminal.

The distribution already knows how to install Podman. Every supported family has
a package manager, a package named `podman`, and repositories the user already
trusts for their system updates. What the installer lacks is not knowledge but
permission.

Two rules from the existing design pull in opposite directions here:

- the install must be **fail-closed** and must not silently change the machine;
- the install must be **one command**, and stopping at the first missing package
  is a failure of that promise.

## Decision

**The installer may offer to install a missing prerequisite with the
distribution's own package manager, and only after the user asks for it and
confirms the exact commands.**

The rules, in order of importance:

1. **Nothing runs by default.** The preflight screen gains one line —
   `Press f and the installer can install it for you` — and one footer key. The
   confirmation screen prints every argument vector that will run. `y` runs
   them; Enter, `n`, and Escape cancel. `--yes` never provisions.
2. **The plan is derived, not guessed.** `/etc/os-release` is read for `ID` and
   `ID_LIKE`; a family is chosen from a fixed table; the package manager must be
   present; and `sudo` must be present when the process is not root. An unknown
   distribution or a missing tool produces no plan at all, and the screen falls
   back to the manual instructions. There is no "try apt, then dnf" fallback.
3. **Commands are argument vectors.** The installer never builds a shell string
   and never runs a shell: `sudo`, `dnf`, `install`, `-y`, `podman` is executed
   as `argv`. Nothing read from `os-release` or a terminal can become syntax.
4. **The terminal is handed over, not shared.** Before the child runs, the
   interface retires its key reader, leaves the alternate screen, and restores
   the original terminal settings. `sudo` prompts for a password the way it
   always does, in the clear, with echo on. The installer never reads, stores, or
   forwards the password, and it takes the terminal back afterwards.
5. **The install itself still never escalates.** `install.sh` runs no privileged
   command, and `scopra install` writes only into the install directory and the
   user unit. Provisioning happens before that, as a separate, deliberate act.
6. **Failure is reported, not hidden.** The command's output stays on the screen
   (the alternate screen is gone while it runs), and a non-zero exit returns the
   interface to the preflight with the failure line and the manual commands. A
   package that is too old for Scopra — Ubuntu 22.04 ships Podman 3.4 — shows up
   as the same failed check, with the version, after the attempt.

The plan today covers Debian/Ubuntu (`apt-get` with `uidmap`, `slirp4netns`, and
`fuse-overlayfs`, which rootless Podman needs there), Fedora/RHEL and
derivatives (`dnf`), Arch (`pacman`), openSUSE (`zypper`), and Alpine (`apk`).

### Amendment: the second kind of prerequisite

Installing the package was not enough. A machine can have Podman 4.0+, run
rootless, use cgroups v2, and still stop the preflight, because systemd's
`user@.service` delegates only `memory` and `pids` by default and Podman cannot
enforce a CPU limit without the `cpu` controller. The user who reported the
first version of this feature hit exactly that: the package install succeeded,
the interface came back, and the only thing left was a manual fix.

The same consent mechanism now covers it. When Podman is present and its own
`podman info` reports that delegation is the *only* missing requirement, `f`
offers a systemd drop-in instead of a package:

```text
[Service]
Delegate=cpu cpuset io memory pids
```

at `/etc/systemd/system/user@.service.d/99-scopra-delegate.conf`, followed by
`systemctl daemon-reload`. Three rules keep it inside the agreement:

1. **No shell redirection, even for a file.** The drop-in is written to the
   user's state directory first, then installed by `mkdir`, `cp`, and `chmod`
   argument vectors. The screen prints those commands, so the file the user
   approves is the file that lands.
2. **Only when it can work.** The offer requires Podman 4.0+, rootless, cgroups
   v2, and `systemctl`; on cgroups v1 the fix would be a boot parameter and a
   reboot, which this installer does not touch.
3. **Say when it takes effect.** A delegation change applies when the user
   manager next starts. The installer re-runs the preflight after the commands;
   if the controllers are still missing, the screen says to log out and back in
   rather than pretending the fix failed. That is the one step this design
   cannot automate without destroying the session it is running in.
4. **A fix that is already installed is not offered again.** The installer reads
   the drop-in before it offers anything, so a session that is waiting for its
   next login is told exactly that, once, with no privileged command to repeat.
   After the commands succeed it also verifies the file it claims to have
   installed, and reports a drop-in it cannot see as a failure.

   The first shipped version of rule 3 got this wrong: the re-check failed in
   the same session, the screen offered `f` again, and pressing it re-ran the
   same four commands. The state the installer could not reach was the one it
   was already in, and it has to say so instead of looping.

## Consequences

- The one command now ends in a working install on the distributions Scopra
  supports, including a machine where Podman was missing, without the user
  leaving the terminal.
- The installer can change the system in a way it could not before. That is a
  real trust expansion, bounded by: it is opt-in, it is visible before it runs,
  it runs only the distribution's package manager against the distribution's
  repositories, and it requires an explicit `y` that cannot be a stray Enter.
- `sudo` is now something the installer can invoke. The documentation,
  `install.sh`'s header, the welcome screen, and ADR-016 all had to stop saying
  "the installer never runs sudo" and start saying "the install never runs sudo;
  a missing prerequisite is offered with the exact command".
- Provisioning is Linux-only, like everything else. macOS still exits before it
  downloads anything.
- A distribution with a Podman older than 4.0 is not fixed by this: the plan
  installs what the repositories have, and the check then honestly reports the
  version. Solving that needs a second source (backports, a newer release, or a
  static build) and a trust decision this ADR does not make.
- The sandbox image is still the user's choice. The installer does not pull
  images: a digest is a security decision, and Mio's rule is that what runs is
  exactly what is already in local storage.

## Alternatives considered

- **Keep printing instructions only.** Rejected: it is the status quo that this
  ADR exists to fix, and it fails exactly the user who is new to Podman.
- **Install without asking.** Rejected without much debate: silent privilege
  escalation from a `curl | sh` one-liner is the pattern the project exists to
  avoid.
- **Ask for the password inside the interface.** Rejected: it would mean owning
  a credential prompt, and a bug there leaks a password. Letting `sudo` do its
  own job on a real terminal is both smaller and more familiar.
- **Download a static Podman or a release binary.** Rejected: an unverified
  binary from a non-distribution source, outside the user's update path, is a
  worse trust story than the distribution's own package.
- **Auto-detect the package manager by trying each one.** Rejected: a failed
  `dnf` on Debian is noise, and "try until something works" is not a security
  posture.
- **Provision the sandbox image too.** Deferred, not rejected: pulling an image
  needs a digest the user chose, and this installer does not get to choose what
  the sandbox runs.
