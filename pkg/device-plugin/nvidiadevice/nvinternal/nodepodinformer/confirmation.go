/*
Copyright 2026 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nodepodinformer

import "time"

// ConfirmationBackoff delays incomplete rounds, including successful queries whose results
// cannot finish cleanup. The owner must serialize access. Its zero value uses
// a five-second initial delay; no method sleeps or retains a Pod snapshot.
type ConfirmationBackoff struct {
	Initial time.Duration
	delay   time.Duration
	next    time.Time
}

// Ready reports whether another confirmation round may start.
func (b *ConfirmationBackoff) Ready(now time.Time) bool { return !now.Before(b.next) }

// Finish records the whole round's outcome, measuring delay from its completion.
// Partial progress is incomplete. Retrying remains possible after the one-minute cap.
func (b *ConfirmationBackoff) Finish(now time.Time, complete bool) {
	if complete {
		b.Reset()
		return
	}
	if b.delay == 0 {
		initial := b.Initial
		if initial <= 0 {
			initial = 5 * time.Second
		}
		b.delay = min(initial, time.Minute)
	} else {
		b.delay = min(2*b.delay, time.Minute)
	}
	b.next = now.Add(b.delay)
}

// Reset clears retry history when no confirmation work remains.
func (b *ConfirmationBackoff) Reset() { b.delay = 0; b.next = time.Time{} }
