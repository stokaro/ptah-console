// Package wire holds what both ends of the agent channel agree on.
package wire

import "time"

// Protocol is the version of the channel the agent and the console speak.
const Protocol = 1

// Ping is how each end tells a live peer from a frozen one: after hearing
// nothing for Ping it sends an HTTP/2 PING, and it closes the connection when
// no answer comes within Ping more. A peer whose process stopped, or whose
// path drops packets without a word, is noticed in twice Ping, not when TCP
// gives up minutes later; the console stops sending it requests then.
const Ping = 15 * time.Second
