#!/bin/bash -ex

main() {
    local pkgs=("tests" "compose_generator")
    pipenv run isort --profile black "${pkgs[@]}"
    pipenv run black "${pkgs[@]}"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
