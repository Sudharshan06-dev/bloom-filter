# Bloom Filter — Four Concepts You Must Understand

---

## 1. What "Pairwise Independent" Hash Functions Means

### The plain English version

Imagine you have a function that maps names to numbers.

- "pairwise independent" means: **knowing what one key hashes to tells you absolutely nothing about what another key hashes to.**

They are *statistically unrelated*. The output of one does not predict the output of any other.

### The formal definition

A family of hash functions H is pairwise independent if for any two **distinct** keys x and y, and any two output positions a and b:

```
P[ h(x) = a  AND  h(y) = b ]  =  1/m²
```

Where m is the size of the output space. This says: the probability that x maps to a *and* y maps to b simultaneously is exactly what it would be if they were completely random and unrelated. No correlation whatsoever.

### Why it matters for bloom filters

A bloom filter sets k bit positions per key. For the **false positive rate formula** to be mathematically valid:

```
FPR ≈ (1 − e^(−kn/m))^k
```

...the formula **assumes** the k positions for any given key are uniformly distributed and independent of positions chosen for other keys. If your hash functions are correlated, this formula breaks down — your actual FPR will be higher than predicted, and you won't know by how much.

### Why "different seeds = same function" is NOT independence

Consider this:

```go
// NOT independent — same mixing logic, different starting point
h1 = murmur3(seed=1, key)
h2 = murmur3(seed=2, key)
```

MurmurHash3's internal mixing is deterministic math. If you know h1 output for key "foo", you can often predict h2 output for "foo" because both are derived from the same linear transformations on the same input. They are *correlated* — not independent. The word "different seed" just shifts the output, it doesn't break the underlying relationship.

**True independence requires that the hash functions come from a family designed with mathematical independence guarantees** — or that you use the Kirsch-Mitzenmacher approximation described next.

---

## 2. The Kirsch-Mitzenmacher Double Hashing Scheme

### The problem it solves

You need k probe positions per key. The naive approach:
- Call k different hash functions → **k × cost per lookup**
- Those functions aren't truly independent anyway (see above)

The paper "Less Hashing, Same Performance" (Kirsch & Mitzenmacher, 2006) proved something surprising:

> **You only need TWO hash values. You can derive all k probe positions from them with no meaningful accuracy loss.**

### The scheme

```
h_i(key) = ( h1(key) + i × h2(key) ) mod m       where i = 0, 1, 2, ..., k-1
```

- `h1` and `h2` are two independent hash values for the key
- `m` is the total number of bits in the filter
- `i` is the probe index
- You get k positions total, one hash call total

### In code (your implementation)

```go
h1, h2 := murmur3.Sum128(key)   // one call, 128-bit output split into two 64-bit halves

for i := 0; i < k; i++ {
    bitIndex = (h1 + uint64(i) * h2) % uint64(m)
}
```

The 128-bit murmur output is split: lower 64 bits → h1, upper 64 bits → h2. These two halves are statistically independent enough that the scheme works in practice.

### Why this works mathematically

The scheme is an application of **linear probing in a hash space**. You're walking a straight line through the m-bit address space with step size h2. As long as h2 ≠ 0 and the step size is relatively prime to m, you'll visit k distinct positions before cycling. The paper proves the FPR from this scheme is asymptotically identical to using k fully independent hash functions.

### The performance win

| Approach | Hash calls per Add/Exists |
|---|---|
| k separate hash functions | k calls |
| Kirsch-Mitzenmacher | **1 call always** |

At k=7 (typical for 1% FPR), you've eliminated 6 out of 7 hash computations. For a bloom filter doing millions of lookups per second, this is enormous.

---

## 3. Why `h2(k) = 0` Is a Degenerate Case

### What happens when h2 = 0

Substitute into the formula:

```
h_i = (h1 + i × 0) mod m  =  h1 mod m     for ALL values of i
```

Every single probe lands on **the same bit position**. You're checking/setting one bit k times instead of k different bits. The bloom filter degenerates into a 1-bit lookup.

### The consequence for FPR

A key is considered "possibly present" only when **all k probed bits are set**. If all k probes hit the same position, that position only needs to be set by *any* previously inserted key to trigger a false positive. The false positive rate shoots up to the fill ratio of that single bit — potentially very high.

The RocksDB implementation had this flaw:

```
// From bloom_impl.h — the legacy code
uint32_t delta = (h >> 17) | (h << 15);   // rotate h by 17 bits to get delta (h2)
```

This rotation has a **1/512 chance of producing delta = 0** (when the specific bit pattern aligns). With millions of keys, you'll hit this regularly. RocksDB measured this caused an absolute **~0.1% FPR floor** — no matter how many bits per key you allocated, you could never get below 0.1% FPR. This was reported in GitHub issue #4120 and is now fixed in the modern `FastLocalBloomImpl`.

### The fix

Ensure h2 is always **odd** — odd numbers are always relatively prime to any power-of-2 modulus, guaranteeing you visit distinct positions:

```go
h2 = h2 | 1    // force the least significant bit to 1 → always odd → never zero
```

This is a one-bit operation with zero performance cost, and it permanently eliminates the degenerate case.

### Proof that odd step → distinct positions

If m is a power of 2 (e.g., 1024 bits), and h2 is odd, then gcd(h2, m) = 1 because:
- m = 2^n has only 2 as a prime factor
- h2 is odd → h2 has no factor of 2
- therefore they share no common factors

By the theory of cyclic groups: when gcd(step, size) = 1, a linear walk of `size` steps visits every position exactly once before cycling. Your k probes (where k << m) are therefore guaranteed distinct.

---

## 4. Why MurmurHash3 / XXHash Over MD5 / SHA

### The core answer

**Bloom filters don't need security. They need speed and uniform distribution.**

MD5 and SHA were designed to be:
- **Collision resistant** — computationally infeasible to find two inputs with the same output
- **Preimage resistant** — given a hash, impossible to find the input
- **Cryptographically secure** — resistant to adversarial attack

None of these properties matter for a bloom filter. You're not protecting data. You just need bits to spread uniformly across your bit array so that different keys hit different positions.

### The performance gap

On modern hardware, rough throughput benchmarks (higher = better):

| Hash Function | Throughput (GB/s) | Designed For |
|---|---|---|
| SHA-256 | ~0.5 GB/s | Cryptographic security |
| MD5 | ~1.0 GB/s | Cryptographic security |
| MurmurHash3 | ~5–8 GB/s | Speed + distribution |
| XXHash64 | ~10–15 GB/s | Speed + distribution |

**SHA-256 is 10–30× slower than MurmurHash3** on the same hardware. For a bloom filter sitting on the read hot path of an SST file lookup — potentially millions of times per second — this difference is the entire budget.

### What "uniform distribution" means and why it's the only thing that matters

For a bloom filter to achieve its theoretical FPR, each bit position `[0, m)` must be equally likely to be probed for any given key. If the hash function clusters outputs — e.g., keys starting with "a" all hash near position 0 — your bit array has hot zones and cold zones. Hot zones fill up fast → FPR in that zone explodes. Cold zones are wasted memory.

MurmurHash3 and XXHash both have **excellent avalanche properties**: flipping one input bit flips ~50% of output bits randomly. This means similar keys (like sequential UUIDs or incremented integers) produce completely uncorrelated hash values — exactly what a bloom filter needs.

Cryptographic hashes also have good avalanche, but they achieve it through many more mixing rounds than necessary — rounds designed to withstand adversarial analysis, not to be fast.

### Why MurmurHash3 specifically in your case

`murmur3.Sum128` gives you a **128-bit output in one call**, which you split into h1 and h2 for the Kirsch-Mitzenmacher scheme. This is architecturally clean — one fast call gives you everything you need. XXHash3 also provides 128-bit output and is faster, but MurmurHash3 is more commonly found in LSM tree implementations (RocksDB, Cassandra, HBase all use it) which makes reading those codebases easier.

---

## Summary — The Mental Model

```
Pairwise independence  →  why we need TWO truly unrelated hash values (not k correlated ones)
Kirsch-Mitzenmacher    →  how we get k probe positions from those two values cheaply
h2 ≠ 0 fix            →  ensures those k positions are always distinct
MurmurHash3 / XXHash   →  gives us the two values fast, with good distribution
```

These four ideas form a complete chain. Each one depends on the previous. You can't understand why Kirsch-Mitzenmacher works without understanding independence. You can't understand the h2=0 bug without understanding the scheme. You can't appreciate the hash function choice without knowing what property (distribution, not security) you actually need.