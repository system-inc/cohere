---
title:   Embedded code
tags: [ a,  b ]
nested:
    key:   value
---

# Embedded code

Each fence below names a language with a native printer, so its contents are formatted as that language.

```ts
const   value : number=1
function add(a:number,b:number){return a+b}
```

```tsx
const element = <div   className="a">{ value }</div>
```

```js
let list = [1,2,3].map(x=>x*2)
```

```json
{"a":1,"b":[true,false,null],
  "c":{"d":"e"}}
```

```css
a{color:red;background:blue}
.b  >  .c{margin:0 auto}
```

```scss
$color : red;
.a{ .b{color:$color} }
```

```yaml
key:   value
list:
    - one
    -   two
```

```graphql
query { user(id:1){ name, friends{name} } }
```

A fence with no language is left as written:

```
const   untouched = 1
```

A fence with an unknown language is left too:

```not-a-language
whatever   goes
```

- a fence inside a list item:

  ```json
  {"inside":  "a list"}
  ```

> ```css
> a{color:red}
> ```
