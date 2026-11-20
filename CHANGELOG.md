# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-11-20

First public release.

### Added

- Template engine: `Compile` / `CompiledTemplate` (JSON output) and `CompileString` /
  `CompiledString` (plain strings), executed against a `Context`.
- Placeholders `data[N]`, `data.path` (JSON via gjson, XML with indexes, `[key=value]`
  predicates and `@attr`), `header["…"]`, `cookie["…"]`, `query["…"]`, `flow_id`, `timestamp`,
  `uuid`.
- Chainable methods `format`, `toUnix`, `unixFormat`, `toInt`, `hexToInt`, `toBool`,
  `toLower`, `toUpper`, `trim`, `replace`, `substr`, `host` (up to five per placeholder).
- `{{#if}}` / `{{#else if}}` / `{{#else}}` / `{{/if}}` blocks with `== != > < >= <= in`.
- Automatic JSON string escaping and unquoted numeric/boolean output.
- `Context.WithParsedXML`, `ParseXMLTree`, `NavigateXMLTree`, `ResolveXMLPosition`.
- `Canonicalize`, `ValidateRef`, `RefMode`, `SourceFormat`, `Version`.
- Package documentation, runnable `Example` functions, `examples/` programs, CI, Makefile.

