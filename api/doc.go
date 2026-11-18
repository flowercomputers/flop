// Package api is the wire format of spade's API as flop uses it: what flop
// sends, and the answers it prints (--json prints these types as they are).
// Spade answers from these same declarations (its own api package names each
// one), so the two cannot drift.
//
// Spade is Flower Computer's control plane, the API flop talks to. A box is
// the machine a bog runs on, and its peater the agent there that runs the bog
// and reports how it is.
package api
