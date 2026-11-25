package allowed

// Whether a digit opens a word of its own is a guess too, so base_64_encoder and base64_encoder both name it.
type Base64Encoder struct {
	padding bool
}

func (encoder Base64Encoder) Padded() bool {
	return encoder.padding
}
