Prose and inline constructs
===========================

## Emphasis   ##

Some *emphasis*, some __strong__, some ***both***, and some ~~struck~~ text. A word_with_underscores stays
as it is, and so does snake*case*inside.

A paragraph with a hard break  
made by two spaces, and another\
made by a backslash.

## Links and references

An [inline link](https://example.com), one with a [title](https://example.com 'single quoted'), an
autolink <https://example.com/path>, a bare www.example.com, and an email <someone@example.com>.

A [full reference][ref], a [collapsed reference][], and a [shortcut reference].

[ref]: https://example.com/ref   "The title"
[collapsed reference]: <https://example.com/with space>
[Shortcut Reference]:
  https://example.com/shortcut
  'over lines'

![An image](image.png "Image title") and ![a reference image][ref].

## Footnotes

Text with a footnote[^note] and another[^2].

[^note]: The note's text,
    continued on an indented line.

[^2]: A second note.

    With a second paragraph.

## Headings of other kinds

Setext two
----------

#### Closed heading ####

## HTML

<details>
<summary>More</summary>

Hidden *markdown* inside.

</details>

Inline <kbd>Ctrl</kbd> + <kbd>C</kbd> html.

<!-- a comment -->

## Escapes and entities

\*not emphasis\*, \# not a heading, a &amp; b, &copy; 2026, &#42; and 1\. not a list.

## Math, liquid and wiki links

Inline $a^2 + b^2$ math, and a block:

$$
\sum_{i=1}^n i
$$

{% include note.html %} and {{ page.title }} and [[Wiki Link]] and [[Target|Alias]].
