Tools for Konflux-CI
===============================================
This repository includes various tools that are used within Konflux-CI
Pipelines. The included tools are, for the most part, written in Python.

## Installation

Prerequisites and container workflow: see [CONTRIBUTING.md](CONTRIBUTING.md).

```bash
pipenv sync
```

## Usage

```bash
pipenv run rpm_verifier --help
```

Full runs need `skopeo` (and related tools) on `PATH`, or use the container workflow in [CONTRIBUTING.md](CONTRIBUTING.md).

## Development

See [AGENTS.md](AGENTS.md) for commands and conventions.

```bash
pipenv run pytest tests
```
