# Tables

A table whose cells are padded unevenly, which the printer realigns:

|Name|Kind|Size|
|-|:-:|--:|
|alpha|letter|1|
|beta gamma delta|letters| 22 |
|  e  |vowel|333|

Pipes at neither end, and a row shorter than the head:

Column one | Column two | Column three
--- | --- | ---
a | b
c | d | e

Escaped pipes, and pipes inside inline code:

| Expression | Means |
| :--------- | ----- |
| `a \| b` | either |
| a \| b | also either |
| `c` | just c |

Wide characters, which count twice toward a column's width:

| 名前 | 説明 | 🍐 |
|---|---|---|
| 中文 | 汉字和标点，混在一起 | yes |
| abc | 한국어 문장 | no |

A table too wide for the print width, which keeps its rows on one line each:

| First heading that is long | Second heading that is longer still | Third | Fourth heading here |
| --- | --- | --- | --- |
| a cell with several words in it | another cell with many more words in it than that | c | the last cell |

Emphasis, links and code inside cells:

| Construct | Example |
|:--|:--|
| emphasis | *a* and __b__ |
| link | [text](https://example.com "title") |
| image | ![alt](image.png) |
| code | `` a`b `` |

> | quoted | table |
> |--|--|
> | in a | block quote |

- a list item holding a table:

  | x | y |
  |-|-|
  | 1 | 2 |
