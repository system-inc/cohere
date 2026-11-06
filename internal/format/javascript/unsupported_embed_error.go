package javascript

type unsupportedEmbedError string

func (language unsupportedEmbedError) Error() string {
	return "no native printer embeds " + string(language) + " in a template literal yet"
}
