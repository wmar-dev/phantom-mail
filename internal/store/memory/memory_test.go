package memory_test

import (
	"testing"

	"phantom-mail/internal/store"
	"phantom-mail/internal/store/memory"
	"phantom-mail/internal/store/storetest"
)

func TestMemoryStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T, opts store.Options) store.Store {
		return memory.New(opts)
	})
}
