.PHONY: build install index check init clean

build:
	go build -o repomap .

# Install the binary on your PATH so you can run `repomap` from any project.
install:
	go install .

# Run against the current directory.
index: build
	./repomap build .

check: build
	./repomap check .

init: build
	./repomap init .

clean:
	rm -f repomap
