// Package gamma is one of four packages in the large-budget-pressure
// fixture. The fixture task mentions the Processor symbol from
// every package so all four score above the viability threshold;
// the tight budget forces the selector to demote most of them.
package gamma

// Processor is the per-package aggregate.
type Processor struct {
	name string
	buf  []byte
}

// Process handles a batch. Padded with a comment block to inflate
// the file's token cost so the selector has something to demote.
//
// gamma lorem ipsum dolor sit amet consectetur adipiscing elit
// sed do eiusmod tempor incididunt ut labore et dolore magna
// aliqua ut enim ad minim veniam quis nostrud exercitation
// ullamco laboris nisi ut aliquip ex ea commodo consequat duis
// aute irure dolor in reprehenderit in voluptate velit esse
// cillum dolore eu fugiat nulla pariatur excepteur sint occaecat
// cupidatat non proident sunt in culpa qui officia deserunt
// mollit anim id est laborum.
func (p *Processor) Process(batch []byte) error {
	p.buf = append(p.buf, batch...)
	return nil
}

// Close releases resources owned by the Processor.
func (p *Processor) Close() error { return nil }

// NewProcessor builds a fresh Processor.
func NewProcessor(name string) *Processor {
	return &Processor{name: name}
}
