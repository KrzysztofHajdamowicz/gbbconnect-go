package modbustcp

import "math/rand/v2"

type sequenceGenerator struct {
	initialized bool
	value       uint16
	initial     func() uint16
}

func newSequenceGenerator() sequenceGenerator {
	return sequenceGenerator{
		initial: func() uint16 {
			return uint16(rand.Uint32()%65535 + 1)
		},
	}
}

func (generator *sequenceGenerator) next() uint16 {
	if !generator.initialized {
		generator.value = generator.initial()
		if generator.value == 0 {
			generator.value = 1
		}
		generator.initialized = true
		return generator.value
	}

	generator.value++
	return generator.value
}
