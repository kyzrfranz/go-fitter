package json

// Option is Converter's option.
type Option func(o *options)

func defaultOptions() *options {
	return &options{
		channelBufferSize:         1000,
		useRawValue:               false,
		printOnlyValidValue:       false,
		printGPSPositionInDegrees: false,
		prettyPrint:               true,
		noRecords:                 false,
	}
}

func WithChannelBufferSize(size int) Option {
	return func(o *options) {
		if size > 0 {
			o.channelBufferSize = size
		}
	}
}

func WithPrintOnlyValidValue() Option {
	return func(o *options) { o.printOnlyValidValue = true }
}

func WithPrintGPSPositionInDegrees() Option {
	return func(o *options) { o.printGPSPositionInDegrees = true }
}

func WithPrettyPrint(pretty bool) Option {
	return func(o *options) { o.prettyPrint = pretty }
}

func WithNoRecords() Option {
	return func(o *options) { o.noRecords = true }
}
