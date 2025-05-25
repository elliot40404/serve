set windows-shell := ["pwsh.exe", "-NoLogo", "-Command"]

default: build

build_cmd := if os() == "windows" { "go build -o ./bin/serve.exe ." } else { "go build -o ./bin/serve ." }

build: clean lint
    {{build_cmd}}

install:
    go install .

build-run: build

rmcmd := if os() == "windows" { "mkdir ./bin -Force; Remove-Item -Recurse -Force ./bin" } else { "rm -rf ./bin" }

clean:
    {{rmcmd}}

lint:
    golangci-lint run

lint-fix:
    golangci-lint run --fix

vendor:
    go mod tidy
    go mod vendor
    go mod tidy

release:
    goreleaser release --snapshot --clean

run *args:
    go run . {{args}}
