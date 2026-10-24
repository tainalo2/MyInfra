package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"myinfra/internal/store"
)

// Purchases and destructions take two calls. The first does nothing: it
// gives a recap and a code. The second, with the same arguments and the
// code, does it, if the code is the token's own, for this tool and these
// arguments, still live and unused, and if what the recap showed (prices,
// records) has not changed since.

const confirmationLife = 10 * time.Minute

// confirmAlphabet leaves out letters and digits read alike (0/O, 1/I/L).
const confirmAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func newConfirmationCode() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	code := make([]byte, 0, 7)
	for i, b := range buf {
		if i == 3 {
			code = append(code, '-')
		}
		code = append(code, confirmAlphabet[int(b)%len(confirmAlphabet)])
	}
	return string(code)
}

// argsHash is the hash of the arguments without the confirmation code: Go
// writes map keys sorted, so the same arguments give the same hash.
func argsHash(raw json.RawMessage) string {
	var args map[string]any
	_ = json.Unmarshal(raw, &args)
	delete(args, "confirmation")
	canonical, _ := json.Marshal(args)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func fingerprintOf(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// confirmed runs the two calls. recap returns the text shown to the person
// and a fingerprint of what it depends on; act does the thing.
func (c *mcpCall) confirmed(raw json.RawMessage, code string, recap func() (string, string, error), act func() (string, error)) (string, error) {
	token, ok := tokenOf(c.ctx)
	if !ok {
		return "", failf("Jeton d'accès requis.")
	}
	a := c.api()
	if code == "" {
		return c.offer(token, raw, recap, "")
	}
	taken, err := a.Store.TakeConfirmation(c.ctx, hashToken(normalCode(code)))
	if errors.Is(err, store.ErrNotFound) {
		return "", failf("Code de confirmation inconnu, expiré ou déjà utilisé : relancez sans code pour obtenir un nouveau récapitulatif.")
	}
	if err != nil {
		return "", err
	}
	if taken.TokenID != token.ID || taken.Tool != c.tool.Name || taken.ArgsHash != argsHash(raw) {
		return "", failf("Ce code de confirmation ne correspond pas à cette demande : les arguments doivent être exactement ceux du récapitulatif. Relancez sans code pour un nouveau récapitulatif.")
	}
	text, fingerprint, err := recap()
	if err != nil {
		return "", err
	}
	if fingerprint != taken.Fingerprint {
		return c.offer(token, raw, func() (string, string, error) { return text, fingerprint, nil },
			"La situation a changé depuis le récapitulatif (prix, disponibilité ou enregistrements) : rien n'a été fait. Voici le nouveau récapitulatif.\n\n")
	}
	return act()
}

func (c *mcpCall) offer(token store.APIToken, raw json.RawMessage, recap func() (string, string, error), lead string) (string, error) {
	text, fingerprint, err := recap()
	if err != nil {
		return "", err
	}
	code := newConfirmationCode()
	if err := c.api().Store.CreateConfirmation(c.ctx, hashToken(code), store.Confirmation{
		TokenID: token.ID, Tool: c.tool.Name, ArgsHash: argsHash(raw), Fingerprint: fingerprint,
		ExpiresAt: time.Now().Add(confirmationLife),
	}); err != nil {
		return "", err
	}
	return "", &toolError{pending: true, Message: lead + text +
		"\nRien n'a encore été fait. Montrez ce récapitulatif à l'utilisateur. S'il est d'accord, rappelez " + c.tool.Name +
		" avec exactement les mêmes arguments et confirmation=\"" + code + "\" (valable 10 minutes, une seule fois)."}
}

func normalCode(code string) string {
	out := make([]byte, 0, len(code))
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, byte(r-'a'+'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			out = append(out, byte(r))
		}
	}
	return string(out)
}
