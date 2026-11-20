package mapperkit

// version is the template engine version, kept in sync with the git tag on release.
//
// Services that compile templates in one place (e.g. validation in an API) and execute them in
// another (e.g. a worker) can compare Version() on both sides to make sure they run the same
// template semantics.
const version = "1.0.0"

// Version returns the template engine version. It is bumped together with the git tag on every
// release.
func Version() string {
	return version
}
