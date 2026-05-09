zsh_completion_dir := env_var_or_default("ZSH_COMPLETION_DIR", env_var("HOME") / ".zsh/completions")
bash_completion_dir := env_var_or_default("BASH_COMPLETION_DIR", env_var("HOME") / ".local/share/bash-completion/completions")
fish_completion_dir := env_var_or_default("FISH_COMPLETION_DIR", env_var("HOME") / ".config/fish/completions")

default:
    just --list

test:
    go test ./...

coverage:
    go test -covermode=atomic -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out

coverage-html: coverage
    go tool cover -html=coverage.out -o coverage.html

vet:
    go vet ./...

lint:
    golangci-lint run ./...

race:
    go test -race ./...

build:
    go build .

check: test vet lint build

release-check:
    goreleaser check

release-snapshot:
    goreleaser release --snapshot --clean --skip sign

release-snapshot-signed:
    goreleaser release --snapshot --clean

acceptance target: build
    ./kpg list
    ./kpg connect {{target}} -- psql -c 'select 1'

install:
    go install .

install-completion-zsh: install
    mkdir -p "{{zsh_completion_dir}}"
    kpg completion zsh > "{{zsh_completion_dir}}/_kpg"
    @echo 'Installed zsh completion to {{zsh_completion_dir}}/_kpg'
    @echo 'Ensure this is in ~/.zshrc before compinit: fpath=("{{zsh_completion_dir}}" $fpath)'

install-completion-bash: install
    mkdir -p "{{bash_completion_dir}}"
    kpg completion bash > "{{bash_completion_dir}}/kpg"
    @echo 'Installed bash completion to {{bash_completion_dir}}/kpg'

install-completion-fish: install
    mkdir -p "{{fish_completion_dir}}"
    kpg completion fish > "{{fish_completion_dir}}/kpg.fish"
    @echo 'Installed fish completion to {{fish_completion_dir}}/kpg.fish'

clean:
    rm -f kpg
