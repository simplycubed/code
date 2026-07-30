module github.com/simplycubed/code

go 1.26

// v0.1.2 was tagged from the wrong commit (the release-prep commit was not yet
// pulled), so its version strings and workflow pins point at v0.1.1. The tag
// could not be re-pointed: proxy.golang.org and sum.golang.org had already
// recorded v0.1.2 against that commit, and moving it would make the checksum
// database disagree with the repository. v0.1.3 is the same content, tagged
// correctly.
retract v0.1.2

// v0.1.4 was tagged from the #59 merge commit before the release-prep commit
// was pulled, so it carries the origin/HEAD fix but not the version pins or the
// changelog entry that belong with it. proxy.golang.org had already recorded it
// against that commit, so the tag could not be moved. v0.1.5 is the intended
// release.
retract v0.1.4
