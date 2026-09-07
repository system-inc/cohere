# Imperative handle effects

React Compiler 1.0.0's useImperativeHandle signature reads its callee, freezes
all arguments and creates a Frozen result. Cohere had no signature for it and
used the unknown-call fallback, conditionally mutating and cross-capturing the
ref and factory. That widened the ref's scope across an unrelated stable state
setter and caused a later correctly memoized callback to report.

The signature now uses the same import-origin resolution as other built-in
hooks. The existing table exercises named, renamed, namespace, default and
barrel imports, plus a shadow and an unrelated module. Five genuine React cases
fail before the repair because no arguments freeze. Shadows retain conservative
effects; imported custom hooks retain their separately specified behavior.

The paired memo reproduction passes a destructured ref to useImperativeHandle,
whose factory returns a focus method using a local useRef. A later useCallback
uses only a state setter. React Compiler accepts this and the counterpart without
the imperative handle. Reading a reactive value inside that callback with an
empty dependency array produces one preservation error. Before the fix cohere
reports one false positive in the first case and two errors instead of one in
the bad-dependency control. Afterwards all three match the reference.

InputMultipleSelect's original reference compilation stops earlier on unsupported
try/finally lowering. Removing try wrappers only in a private oracle copy permits
CompileSuccess, while cohere retains the same finding with and without those
wrappers. The permanent reproduction contains no try statement and exposes the
missing signature without depending on a reference bailout. No source application
files, suppression behavior or preservation checks change.

The full lint suite passes without any corpus repin. The population must be
measured from the committed repair, separately from the primitive-property fix.
That preceding commit, 0740825, measured nine to seven findings: two chart
occurrences removed, zero added, seven unchanged. Its archived binary SHA256 is
c04dbf080f69c0a8763f651e2bc1451fef54adbaad46036a37a7da418f9db201.
