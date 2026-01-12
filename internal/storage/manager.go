package storage

import (
	"log"
	"sync"
)

var (
	GlobalStorage Storage
	once          sync.Once
)

func InitStorage(baseDir string) {
	once.Do(func() {
		var err error
		GlobalStorage, err = NewLocalStorage(baseDir)
		if err != nil {
			log.Fatalf("failed to init storage: %v", err)
		}
	})
}

func GetStorage() Storage {
	if GlobalStorage == nil {
		InitStorage("data/sandbox") // Default fallback
	}
	return GlobalStorage
}
