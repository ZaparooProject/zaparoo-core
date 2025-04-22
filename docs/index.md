# Developer Guide

## Environment

Zaparoo Core is written in Go, uses Task and Python for build scripts, and Docker for building all platforms which use Cgo.

Build scripts work on Linux, Mac and Windows (natively or WSL). Just make sure all dependencies are installed and main binaries of them are available in your path.

### Dependencies

- [Go](https://go.dev/)

  Version 1.23 or newer. The build script assumes your Go path is the default location: `$HOME/go` 

- [Task](https://taskfile.dev/)
- [Python](https://www.python.org/)
- [Docker](https://www.docker.com/)

  On Linux, enable cross-platform builds with something like: `apt install qemu binfmt-support qemu-user-static`

  On Mac and Windows, Docker Desktop comes with everything you need already. If you're using WSL, make sure it's using Docker from the host machine.

## Building

To start, you can run `go mod download` from the root of the project folder. This will download all dependencies used by the project. Builds automatically do this, but running it now will stop your editor from complaining about missing modules.

All build steps are done with the `task` command run from the root of the project folder. Run `task --list-all` by itself to see a list of available commands.

Built binaries will be created in the `_build` directory under its appropriate platform and architecture subdirectory.

These are the important commands:

- `task <platform>:build-<architecture>`

  Complete a full build for the given platform and architecture. This will also automatically create any necessary Docker images.

- `task <platform>:deploy-<architecture>`

  Some builds also have a helper command to automatically make a new build, transfer it to a remote device and remotely restart the service running. For example, to enable this for the MiSTer ARM build, add `MISTER_IP=1.2.3.4` to a `.env` file in the root of the project and then run `task mister:deploy-arm`.

## Testing

When changing the application behavior, in particular the reader loop, some testing is required. The [Scan Behavior checklist](./scan-behavior) contains a list of expected behavior for the application under certain conditions. It is useful to test them and ensure we didn't break any flows.
