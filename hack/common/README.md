# Common Scripts Library

The k8s operators in this GitHub org use mostly the same `make` targets and surrounding scripts. This makes sense, because this way developers do not have to think about in which repo they are working right now - `make tidy` will always tidy the go modules.
The drawback is that all `make` targets and scripts have to be kept in sync. If the `make` targets have the same name but a different behavior (conceptually, not code-wise), this will became more of an disadvantage than an advantage. This 'keeping it in sync' means that adding an improvement to any of the scripts required this improvement to be added to all of the script's copies in the different repositories, which is annoying and error-prone.

To improve this, the scripts that are shared between the different repositories have been moved into this repository, which is intended to be used as a git submodule in the actual operator repositories.

Additionally, a Makefile containing some common `make` targets has also been created, which can be included from the repositories' Makefiles.

## Requirements

It is strongly recommended to include this submodule under the `hack/common` path in the operator repositories. While the scripts are generally designed to be able to be called from anywhere and should work with some additional configuration if in a different path, the common Makefile contains some hard-coded references assuming the scripts in this repository to be under `hack/common` and (for now) won't work with another location.

The [environment.sh](./environment.sh) script sets several environment variables and is sourced at the beginning of nearly every other script contained in this repo. Most of these environment variables are only set if they are not yet defined, allowing them to be overwritten easily.
In addition, the environment script looks for a script with the same name (`environment.sh`) in the directory containing this checked-out submodule. If it exists, it is sourced as part of the script. While repo-specific overwrites and new environment variables can easily be injected this way, there are a few environment variables that have to be set for the scripts to work properly and it's recommended to do that in the mentioned `environment.sh` file:
- `MODULE_NAME` must be set to the name of the go module defined in the repository, e.g. `github.tools.sap/CoLa/mcp-operator`.
- `NESTED_MODULES` must contain a comma-separated list of nested go modules contained in the repository. Most operator repos currently have a single nested module named `api`.

The Makefile also requires some variables to be defined when included from another Makefile:
- `REPO_ROOT` must point to the root folder of the repository (similarly to `PROJECT_ROOT` in the scripts).
- `COMPONENTS` must be a comma-separated list of components that are part of the repository. While the scripts are build in a way that supports multiple binaries per repository, the repositories currently only ever contain a single one, so this variable will usually just contain its name, e.g. `mcp-operator`.

Note that the `release-*` targets are contained in the shared Makefile, but depend on the targets `tidy`, `generate`, `verify`, and `test`, of which currently only `tidy` is part of the shared Makefile. The reason is that the other ones use hard-coded paths and are therefore harder to generalize and migrate to the shared Makefile. This means that these steps currently *must* be defined in the repository's Makefile if usage of the `release-*` targets is desired.

## Setup

To use this repository, first check it out via
```shell
git submodule add https://github.tools.sap/CoLa/common-hack-scripts.git hack/common
```
and ensure that it is checked-out via
```shell
git submodule init
```

To use the Makefile contained in this repo, add something like this to the beginning of the repo's Makefile (after the definition of `REPO_ROOT`):
```makefile
COMMON_MAKEFILE ?= $(REPO_ROOT)/hack/common/Makefile
ifneq (,$(wildcard $(COMMON_MAKEFILE)))
include $(COMMON_MAKEFILE)
endif
```

Overwriting `make` targets contained in the shared Makefile is possible by re-declaring them after the `include` statement, but will lead to warnings printed on the console. To avoid the warnings, make sure that no duplicate targets are defined. To help with this, the shared Makefile uses a variable named `<normalized-target-name>_TARGET` for every target that it defines, with `<normalized-target-name>` being the upper-case name of the respective target with special characters replaced by underscores (e.g. the variable for the `generate-docs` target would be `GENERATE_DOCS_TARGET`). In the shared Makefile, the targets are only defined if the corresponding variable is not yet defined. Defining the target will also set the corresponding variable to `true`.

This means that you have the following possibilities to handle overwriting targets:

##### If you want the repository-specific implementation to take precedence
Set `<normalized-target-name>` to any value before the `include` statement. This should prevent the shared Makefile from declaring that target.
```makefile
FOO_TARGET := true # now the shared Makefile won't declare 'foo'
...
# shared Makefile import snippet from above
COMMON_MAKEFILE ?= $(REPO_ROOT)/hack/common/Makefile
ifneq (,$(wildcard $(COMMON_MAKEFILE)))
include $(COMMON_MAKEFILE)
endif
...
# declare foo yourself
.PHONY foo
foo:
  @echo foo
```

##### If you want the shared implementation to take precedence
Wrap your target definition in a conditional expression checking for the corresponding variable:
```makefile
ifndef FOO_TARGET
.PHONY foo
foo:
  @echo foo
endif
```

Note that the help printed by the `make help` command is based on a static evaluation of the Makefile(s) and will therefore also include a help text for targets which are wrapped in unfulfilled conditionals.

## Caveats

The setup described above is not without drawbacks and pitfalls, some of which are explained below.

### Git submodules require effort

Git submodules are nice in theory, but they are neither checked out automatically on `git clone`, nor updated on `git pull`.

After cloning the repo, `git submodule init` has to be called to initially checkout the submodule's content.

For checking out the correct version of the submodule after a `git pull`, the `git submodule update` command is required.

### 'make envtest' cannot be moved into this repo

The `make envtest` target, which is used by some operator repositories, cannot be moved into the common Makefile included in this repository. The reason is that it might be called during tests and apparently the Hyperspace pipeline can't handle git submodules. For this reason, it is kept in the main repo's Makefile and designed to work even without this submodule.

### Missing targets in shared Makefile

As an example, `make generate` does the same thing in all operator repositories *conceptually*, but the actual implementation differs, as it contains hard-coded paths to code folders and the `mcp-operator` one additionally downloads the Gardener API coding. For this and similar reasons, many of the `make` targets that are used during development have not been migrated to the shared Makefile yet. To make this even more confusing, the `prepare-release` target of the shared Makefile depends on the `generate` target and some others which are not yet contained in the shared Makefile. This will hopefully change in the future.
