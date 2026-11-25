interface Event { readonly type: string }
type Listener = (ev: Event) => any;
function listen(fn: Listener): void { void fn; }
const handleKeyDown = (event: Event): void => { void event; };
listen(handleKeyDown);
