package main

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/twmb/murmur3"
)

var hashFns murmur3.Hash128

func init() {
	hashFns = murmur3.New128()
}

type BloomFilter struct {
	filter []uint8
	size   int32
}

// Utility function to create the hash method -> Currently returns integer -> Check for the correct return type
func mumurhash(key string) (uint64, uint64) {
	//Use that hashFn to generate the murmur hash corresponding to the seed values of that particular hashFn
	hashFns.Reset()
	hashFns.Write([]byte(key))
	h1, h2 := hashFns.Sum128()
	return h1, h2
}

// Create bloom filter with size
func NewBloomFilter(size int32) *BloomFilter {
	return &BloomFilter{
		filter: make([]uint8, (size+7)/8),
		size:   size,
	}
}

// Create the add function to add in the new string
func (b *BloomFilter) Add(key string, hashFnToConsider int) {

	lower_bits, upper_bits := mumurhash(key) //Divided 128 bit to 64 and 64 bits -> double hashing

	for fnIdx := 0; fnIdx < hashFnToConsider; fnIdx++ {
		bitIndex := (lower_bits + uint64(fnIdx)*upper_bits) % uint64(b.size)
		byteIndex := bitIndex / 8
		bitOffset := bitIndex % 8
		b.filter[byteIndex] |= 1 << bitOffset
	}
}

// Take in the key and get the index by hashing the key to check whether the index exsists or not
func (b *BloomFilter) Exists(key string, hashFnToConsider int) bool {

	lower_bits, upper_bits := mumurhash(key) //Divided 128 bit to 64 and 64 bits -> double hashing

	for fnIdx := 0; fnIdx < hashFnToConsider; fnIdx++ {

		bitIndex := (lower_bits + uint64(fnIdx)*upper_bits) % uint64(b.size)

		//Fail fast method if the key is false then return the false value immediately -> only when the key is true for all the hash fn we return true
		//This will help us to reduce the collisions at first, and once the hashfn increases we will encounter increase in the false positives rates

		byteIndex := bitIndex / 8
		bitOffset := bitIndex % 8

		currentKeyExists := b.filter[byteIndex]&(1<<bitOffset) > 0

		if !currentKeyExists {
			return false
		}
	}

	return true
}

// Exists method to check whether a particular string exists or not
func main() {

	//Add all the keys which includes both the existence keys and non-existence keys
	dataset := make([]string, 0)

	for i := 0; i < 1000; i++ {
		dataset = append(dataset, uuid.New().String())
	}

	for idx := 1; idx < 100; idx++ {

		newBloomFilter := NewBloomFilter(int32(10000))

		//Check the number of false positives and true negatives to get the False postive rate
		var falsePositives float64
		var trueNegatives float64

		//Add the first half of the uuids to the murmurhash
		for i := 0; i < len(dataset)/2; i++ {
			newBloomFilter.Add(dataset[i], idx) //Adding the first half - remaining half will be used to test false positives
		}

		//Now check for the next half which has not been included in the hash function -> track the false positive rates
		for i := len(dataset) / 2; i < len(dataset); i++ {
			uuidExists := newBloomFilter.Exists(dataset[i], idx)

			if uuidExists {
				//fmt.Printf("At index %v it suggests that a UUID is present %v", i, dataset[i])
				falsePositives += 1
			} else {
				trueNegatives += 1
			}
		}

		//FPR rate is about (fp / (fp + tn)) * 100 to get the false positives of this particular hash functions
		fmt.Println("\n", (falsePositives/(falsePositives+trueNegatives))*100)
	}
}
