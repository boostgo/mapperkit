package mapperkit

import "errors"

// Sentinel errors. Errors returned by Compile, CompileString, Execute and ValidateRef wrap one
// of these, so callers can match them with errors.Is.
var (
	// ErrCompile wraps every error returned by Compile.
	ErrCompile = errors.New("mapperkit.compile")
	// ErrUnclosedPlaceholder means a "{{" has no matching "}}".
	ErrUnclosedPlaceholder = errors.New("mapperkit.unclosed_placeholder")
	// ErrInvalidDataIndex means the N in data[N] is not an integer (or is negative in ValidateRef).
	ErrInvalidDataIndex = errors.New("mapperkit.invalid_data_index")
	// ErrUnknownPlaceholder means the placeholder base is not one of data[N], data.path,
	// header["k"], cookie["k"], query["k"], flow_id, timestamp or uuid.
	ErrUnknownPlaceholder = errors.New("mapperkit.unknown_placeholder")
	// ErrUnknownMethod means a chained method name is not supported.
	ErrUnknownMethod = errors.New("mapperkit.unknown_method")
	// ErrResolve means a placeholder could not be resolved at execution time.
	ErrResolve = errors.New("mapperkit.resolve")
	// ErrParseDate means a value could not be parsed as a date.
	ErrParseDate = errors.New("mapperkit.parse_date")
	// ErrInvalidIfCondition means a {{#if}} condition has no operator or a malformed operand.
	ErrInvalidIfCondition = errors.New("mapperkit.invalid_if_condition")
	// ErrUnclosedIfBlock means a {{#if}} block has no matching {{/if}}.
	ErrUnclosedIfBlock = errors.New("mapperkit.unclosed_if_block")
	// ErrIfBlockSyntax means a structural error in an {{#if}} block (stray {{#else}} or {{/if}}, etc.).
	ErrIfBlockSyntax = errors.New("mapperkit.if_block_syntax")
	// ErrTooManyMethods means a placeholder chains more methods than allowed.
	ErrTooManyMethods = errors.New("mapperkit.too_many_methods")
	// ErrInvalidMethodArg means a method argument is malformed (e.g. a non-integer substr offset).
	ErrInvalidMethodArg = errors.New("mapperkit.invalid_method_arg")

	// ErrNotSingleRef is returned by ValidateRef in ModeSingleRef when s is not exactly one placeholder.
	ErrNotSingleRef = errors.New("mapperkit.not_single_ref")
	// ErrIfBlockNotAllowed is returned by ValidateRef in ModeStringTemplate when s contains {{#if}}.
	ErrIfBlockNotAllowed = errors.New("mapperkit.if_block_not_allowed")
	// ErrIndexBaseOnPathFormat is returned by ValidateRef when data[N] is used for a JSON or XML source.
	ErrIndexBaseOnPathFormat = errors.New("mapperkit.index_base_on_path_format")
	// ErrPathBaseOnIndexFormat is returned by ValidateRef when data.path is used for a TSV source.
	ErrPathBaseOnIndexFormat = errors.New("mapperkit.path_base_on_index_format")
	// ErrDataBaseOnTextFormat is returned by ValidateRef when any data reference is used for a text source.
	ErrDataBaseOnTextFormat = errors.New("mapperkit.data_base_on_text_format")
)
