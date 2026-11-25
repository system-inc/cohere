type Effect = () => void | (() => void);
function effect(fn: Effect): void { fn(); }
effect((): void => {});
