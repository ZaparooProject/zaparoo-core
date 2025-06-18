# Developer Guide

## Environment

Zaparoo Core is written in Go, uses Task for build scripts, and Docker for building all platforms.

Build scripts work on Linux, Mac and Windows (natively or WSL). Just make sure all dependencies are installed with Go, Task and Docker binaries available in your path.

### Dependencies

- [Go](https://go.dev/)

  Version 1.23 or newer. The build script assumes your Go path is in the default location, for caching between Docker build environments: `$HOME/go` 

- [Task](https://taskfile.dev/)
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

### Direct Builds

Core can be built directly on the host using the `task build` command, but will require some extra dependencies in the environment depending on the platform, to satisfy the Cgo dependency. Docker is not required.

#### Linux

Linux is the most complex because it uses a custom build of libnfc. Check a Dockerfile like `scripts/linux_amd64/Dockerfile` for full details of setting up the environment.

You will need:

- Standard build tools (GCC)
- libnfc build dependencies (check Dockerfile)
- Latest commit of [libnfc repo](https://github.com/nfc-tools/libnfc)

Once these are installed and set up, you can just run `task build` for a generic Linux desktop build or `PLATFORM=<platform> task build` for one of the variant platforms.

#### Windows

Windows doesn't use libnfc so doesn't have as many dependencies, but does need a compiler available for sqlite.

You will need:

- [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)

Then you can build with `NO_LIBNFC=true task build`. The `CC` and `CXX` environment variables can also be set if you need to specify the path to TDM-GCC.

#### Mac

Mac also doesn't use libnfc but does need Xcode for sqlite and some native UI elements. It also cannot produce static builds which is the default.

You will need:

- Install Xcode from the App Store

Then build with `NO_LIBNFC=true NO_STATIC=true task build`. This will produce a binary for your host's architecture, not a universal binary or .app folder.

## Testing

When changing the application behavior, in particular the reader loop, some testing is required. The [Scan Behavior checklist](./scan-behavior) contains a list of expected behavior for the application under certain conditions. It is useful to test them and ensure we didn't break any flows.
