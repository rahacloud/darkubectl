binary := "darkubectl"
bin_dir := "bin"

# list the recipes
[private]
default:
    @just --list

# build all Go packages
[group('build')]
build:
    go build ./...

# build the CLI into bin/
[group('build')]
binary:
    @mkdir -p {{ bin_dir }}
    go build -o {{ bin_dir }}/{{ binary }} .

# install the CLI into the Go binary directory
[group('build')]
install:
    go install .

# remove compiled binaries
[group('build')]
clean:
    rm -rf {{ bin_dir }}

# format Go source files
[group('develop')]
fmt:
    gofmt -w .

# run golangci-lint (CI uses v2.14.0)
[group('quality')]
lint:
    golangci-lint run ./...

# run tests with the race detector
[group('quality')]
test:
    go test -race ./...

# lint Markdown files, with the version CI uses (needs npx)
[group('quality')]
lint-md:
    npx --yes markdownlint-cli2@0.23.3 "**/*.md"

# check that Go source files are formatted
[group('quality')]
fmt-check:
    @test -z "$(gofmt -l .)" || { echo 'Go files need formatting:'; gofmt -l .; exit 1; }

# run the checks CI runs: format, lint, build, test
[group('quality')]
check: fmt-check lint build test
