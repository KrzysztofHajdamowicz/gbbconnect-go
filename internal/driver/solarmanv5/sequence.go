package solarmanv5

import "math/rand/v2"

type sequenceGenerator struct {
	initialized bool
	value       byte
	initial     func() byte
}

func newSequenceGenerator() sequenceGenerator {
	return sequenceGenerator{
		initial: func() byte {
			return byte(rand.Uint32()%255 + 1)
		},
	}
}

func (generator *sequenceGenerator) next() byte {
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
