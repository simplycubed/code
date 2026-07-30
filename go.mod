module github.com/simplycubed/code

go 1.26

// v0.1.2 was tagged from the wrong commit (the release-prep commit was not yet
// pulled), so its version strings and workflow pins point at v0.1.1. The tag
// could not be re-pointed: proxy.golang.org and sum.golang.org had already
// recorded v0.1.2 against that commit, and moving it would make the checksum
// database disagree with the repository. v0.1.3 is the same content, tagged
// correctly.
retract v0.1.2
