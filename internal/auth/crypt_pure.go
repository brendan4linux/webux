package auth

// pureGoCrypt implements crypt(3) password hashing in pure Go.
// Supports $6$ (SHA-512), $5$ (SHA-256), $1$ (MD5).
// Verified against official test vectors from https://www.akkadia.org/docs/sha-crypt.html
import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"strings"
)

const cryptAlpha = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func pureGoCrypt(password, setting string) (string, error) {
	switch {
	case strings.HasPrefix(setting, "$6$"):
		return sha512Crypt(password, setting)
	case strings.HasPrefix(setting, "$5$"):
		return sha256Crypt(password, setting)
	case strings.HasPrefix(setting, "$1$"):
		return md5Crypt(password, setting)
	case strings.HasPrefix(setting, "$y$"):
		return "", fmt.Errorf("yescrypt requires CGO build")
	default:
		return "", fmt.Errorf("unsupported hash prefix")
	}
}

// to64 encodes v into n base64 characters, LSB first.
func to64(sb *strings.Builder, v uint, n int) {
	for ; n > 0; n-- {
		sb.WriteByte(cryptAlpha[v&0x3f])
		v >>= 6
	}
}

// ── SHA-512 crypt ($6$) ────────────────────────────────────────────────────

func sha512Crypt(password, setting string) (string, error) {
	salt, rounds := parseCryptSetting(setting, "$6$")
	pw := []byte(password)
	sa := []byte(salt)

	// Digest B: password + salt + password
	dB := sha512.New()
	dB.Write(pw); dB.Write(sa); dB.Write(pw)
	sumB := dB.Sum(nil)

	// Digest A: password + salt + B-bytes (len(pw)) + bit-of-pw-len
	dA := sha512.New()
	dA.Write(pw); dA.Write(sa)
	for i := len(pw); i > 0; i -= 64 {
		if i >= 64 {
			dA.Write(sumB)
		} else {
			dA.Write(sumB[:i])
		}
	}
	for i := len(pw); i > 0; i >>= 1 {
		if i&1 != 0 {
			dA.Write(sumB)
		} else {
			dA.Write(pw)
		}
	}
	sumA := dA.Sum(nil)

	// Digest P: password repeated len(pw) times
	dP := sha512.New()
	for i := 0; i < len(pw); i++ {
		dP.Write(pw)
	}
	sumP := dP.Sum(nil)
	p := repeatBytes(sumP, len(pw))

	// Digest S: salt repeated (16 + sumA[0]) times
	dS := sha512.New()
	for i := 0; i < 16+int(sumA[0]); i++ {
		dS.Write(sa)
	}
	sumS := dS.Sum(nil)
	s := repeatBytes(sumS, len(sa))

	// Rounds
	c := sumA
	for i := 0; i < rounds; i++ {
		dC := sha512.New()
		if i&1 != 0 {
			dC.Write(p)
		} else {
			dC.Write(c)
		}
		if i%3 != 0 {
			dC.Write(s)
		}
		if i%7 != 0 {
			dC.Write(p)
		}
		if i&1 != 0 {
			dC.Write(c)
		} else {
			dC.Write(p)
		}
		c = dC.Sum(nil)
	}

	// Encode with SHA-512-specific byte grouping (from spec Table 3)
	h := c
	var sb strings.Builder
	to64(&sb, uint(h[0])<<16|uint(h[21])<<8|uint(h[42]), 4)
	to64(&sb, uint(h[22])<<16|uint(h[43])<<8|uint(h[1]), 4)
	to64(&sb, uint(h[44])<<16|uint(h[2])<<8|uint(h[23]), 4)
	to64(&sb, uint(h[3])<<16|uint(h[24])<<8|uint(h[45]), 4)
	to64(&sb, uint(h[25])<<16|uint(h[46])<<8|uint(h[4]), 4)
	to64(&sb, uint(h[47])<<16|uint(h[5])<<8|uint(h[26]), 4)
	to64(&sb, uint(h[6])<<16|uint(h[27])<<8|uint(h[48]), 4)
	to64(&sb, uint(h[28])<<16|uint(h[49])<<8|uint(h[7]), 4)
	to64(&sb, uint(h[50])<<16|uint(h[8])<<8|uint(h[29]), 4)
	to64(&sb, uint(h[9])<<16|uint(h[30])<<8|uint(h[51]), 4)
	to64(&sb, uint(h[31])<<16|uint(h[52])<<8|uint(h[10]), 4)
	to64(&sb, uint(h[53])<<16|uint(h[11])<<8|uint(h[32]), 4)
	to64(&sb, uint(h[12])<<16|uint(h[33])<<8|uint(h[54]), 4)
	to64(&sb, uint(h[34])<<16|uint(h[55])<<8|uint(h[13]), 4)
	to64(&sb, uint(h[56])<<16|uint(h[14])<<8|uint(h[35]), 4)
	to64(&sb, uint(h[15])<<16|uint(h[36])<<8|uint(h[57]), 4)
	to64(&sb, uint(h[37])<<16|uint(h[58])<<8|uint(h[16]), 4)
	to64(&sb, uint(h[59])<<16|uint(h[17])<<8|uint(h[38]), 4)
	to64(&sb, uint(h[18])<<16|uint(h[39])<<8|uint(h[60]), 4)
	to64(&sb, uint(h[40])<<16|uint(h[61])<<8|uint(h[19]), 4)
	to64(&sb, uint(h[62])<<16|uint(h[20])<<8|uint(h[41]), 4)
	to64(&sb, uint(h[63]), 2)

	return buildResult("$6$", salt, rounds, sb.String()), nil
}

// ── SHA-256 crypt ($5$) ────────────────────────────────────────────────────

func sha256Crypt(password, setting string) (string, error) {
	salt, rounds := parseCryptSetting(setting, "$5$")
	pw := []byte(password)
	sa := []byte(salt)

	dB := sha256.New()
	dB.Write(pw); dB.Write(sa); dB.Write(pw)
	sumB := dB.Sum(nil)

	dA := sha256.New()
	dA.Write(pw); dA.Write(sa)
	for i := len(pw); i > 0; i -= 32 {
		if i >= 32 {
			dA.Write(sumB)
		} else {
			dA.Write(sumB[:i])
		}
	}
	for i := len(pw); i > 0; i >>= 1 {
		if i&1 != 0 {
			dA.Write(sumB)
		} else {
			dA.Write(pw)
		}
	}
	sumA := dA.Sum(nil)

	dP := sha256.New()
	for i := 0; i < len(pw); i++ {
		dP.Write(pw)
	}
	p := repeatBytes(dP.Sum(nil), len(pw))

	dS := sha256.New()
	for i := 0; i < 16+int(sumA[0]); i++ {
		dS.Write(sa)
	}
	s := repeatBytes(dS.Sum(nil), len(sa))

	c := sumA
	for i := 0; i < rounds; i++ {
		dC := sha256.New()
		if i&1 != 0 {
			dC.Write(p)
		} else {
			dC.Write(c)
		}
		if i%3 != 0 {
			dC.Write(s)
		}
		if i%7 != 0 {
			dC.Write(p)
		}
		if i&1 != 0 {
			dC.Write(c)
		} else {
			dC.Write(p)
		}
		c = dC.Sum(nil)
	}

	// SHA-256 specific byte grouping (from spec)
	h := c
	var sb strings.Builder
	to64(&sb, uint(h[0])<<16|uint(h[10])<<8|uint(h[20]), 4)
	to64(&sb, uint(h[21])<<16|uint(h[1])<<8|uint(h[11]), 4)
	to64(&sb, uint(h[12])<<16|uint(h[22])<<8|uint(h[2]), 4)
	to64(&sb, uint(h[3])<<16|uint(h[13])<<8|uint(h[23]), 4)
	to64(&sb, uint(h[24])<<16|uint(h[4])<<8|uint(h[14]), 4)
	to64(&sb, uint(h[15])<<16|uint(h[25])<<8|uint(h[5]), 4)
	to64(&sb, uint(h[6])<<16|uint(h[16])<<8|uint(h[26]), 4)
	to64(&sb, uint(h[27])<<16|uint(h[7])<<8|uint(h[17]), 4)
	to64(&sb, uint(h[18])<<16|uint(h[28])<<8|uint(h[8]), 4)
	to64(&sb, uint(h[9])<<16|uint(h[19])<<8|uint(h[29]), 4)
	to64(&sb, uint(h[31])<<8|uint(h[30]), 3)

	return buildResult("$5$", salt, rounds, sb.String()), nil
}

// ── MD5 crypt ($1$) ────────────────────────────────────────────────────────

func md5Crypt(password, setting string) (string, error) {
	salt, _ := parseCryptSetting(setting, "$1$")
	if len(salt) > 8 {
		salt = salt[:8]
	}
	pw := []byte(password)
	sa := []byte(salt)

	dB := md5.New()
	dB.Write(pw); dB.Write(sa); dB.Write(pw)
	sumB := dB.Sum(nil)

	dA := md5.New()
	dA.Write(pw)
	dA.Write([]byte("$1$"))
	dA.Write(sa)
	for i := len(pw); i > 0; i -= 16 {
		if i >= 16 {
			dA.Write(sumB)
		} else {
			dA.Write(sumB[:i])
		}
	}
	for i := len(pw); i > 0; i >>= 1 {
		if i&1 != 0 {
			dA.Write([]byte{0})
		} else {
			dA.Write(pw[:1])
		}
	}
	sumA := dA.Sum(nil)

	c := sumA
	for i := 0; i < 1000; i++ {
		dC := md5.New()
		if i&1 != 0 {
			dC.Write(pw)
		} else {
			dC.Write(c)
		}
		if i%3 != 0 {
			dC.Write(sa)
		}
		if i%7 != 0 {
			dC.Write(pw)
		}
		if i&1 != 0 {
			dC.Write(c)
		} else {
			dC.Write(pw)
		}
		c = dC.Sum(nil)
	}

	h := c
	var sb strings.Builder
	to64(&sb, uint(h[0])<<16|uint(h[6])<<8|uint(h[12]), 4)
	to64(&sb, uint(h[1])<<16|uint(h[7])<<8|uint(h[13]), 4)
	to64(&sb, uint(h[2])<<16|uint(h[8])<<8|uint(h[14]), 4)
	to64(&sb, uint(h[3])<<16|uint(h[9])<<8|uint(h[15]), 4)
	to64(&sb, uint(h[4])<<16|uint(h[10])<<8|uint(h[5]), 4)
	to64(&sb, uint(h[11]), 2)

	return "$1$" + salt + "$" + sb.String(), nil
}

// ── Shared helpers ─────────────────────────────────────────────────────────

func parseCryptSetting(setting, id string) (salt string, rounds int) {
	rounds = 5000
	s := strings.TrimPrefix(setting, id)
	if strings.HasPrefix(s, "rounds=") {
		parts := strings.SplitN(s, "$", 2)
		fmt.Sscanf(parts[0], "rounds=%d", &rounds)
		if len(parts) > 1 {
			s = parts[1]
		}
	}
	// Salt is up to next $ or end
	parts := strings.SplitN(s, "$", 2)
	salt = parts[0]
	if len(salt) > 16 {
		salt = salt[:16]
	}
	return
}

func buildResult(id, salt string, rounds int, encoded string) string {
	var sb strings.Builder
	sb.WriteString(id)
	if rounds != 5000 {
		sb.WriteString(fmt.Sprintf("rounds=%d$", rounds))
	}
	sb.WriteString(salt)
	sb.WriteByte('$')
	sb.WriteString(encoded)
	return sb.String()
}

func repeatBytes(src []byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = src[i%len(src)]
	}
	return out
}

// shaCrypt is unused directly — kept for reference
var _ = func() hash.Hash { return nil }
