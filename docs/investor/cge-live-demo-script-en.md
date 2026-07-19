# CGE Live Lab script — English

Target duration: five minutes. The points below are observations, not promised
values: say “let’s observe the actual result” if the current policy produces a
different band or association.

## 0. Frame — 20 seconds

Open `/` with an empty session, simulated clock, and the banner
`Synthetic events — real CGE processing`.

Say: “Each click sends a synthetic observation to the real ShadowEngine in a
temporary local directory. This lab has no security action authority.”

## 1. First observation — 40 seconds

Choose `Resident A enters normally`, keep `18:15`, then `Send to CGE`.

Watch observation, context, association plan, candidate chain, insufficient
evidence, and `insufficient_history` deviation before learning.

Say: “At startup, the engine does not pretend to know the routine. It creates
candidate memory and exposes the lack of history.”

## 2. Build a routine — 45 seconds

Click `Repeat this routine · 7` or `Load real baseline · 7 days`. Every event
is sent separately with a 24-hour simulated timestamp.

Watch occurrences, distinct days, revisions, routines, and WAL records.
Say: “The routine is produced by real engine mutations, not inserted as a
finished snapshot.”

## 3. Compare a deviation — 50 seconds

Click `Create a temporal deviation`. The action detects a mature routine,
prepares the following day at `02:15`, keeps its subject and pattern, switches
to `night`, and does not send the event. Review the form before clicking
`Send to CGE`.

Read `EVALUATION BEFORE LEARNING` in the trace and the deviation panel:
band, score, coverage, structure, temporal and interval factors.
The trace should show `Baseline read`, `Deviation evaluated`, then `Routine
occurrence added`. An aligned zero is explicitly a completed evaluation with
no measured difference; it does not mean that no calculation occurred.

Say: “A deviation is an explainable difference from history, not an alarm and
not a probability of danger. The occurrence is then learned according to the
actual engine behavior.”

## 4. Ambiguity — 50 seconds

With a baseline or at least one existing observation, choose `Prepare
ambiguous observation` and send it. If needed, keep the last simulated time.

Watch candidates, margin, `ambiguous`, and the opened hypothesis.

Say: “The CGE can say ‘I do not know yet’. No alternative is selected
automatically.” Explicitly mention `partial` or `missing` context when used.

## 5. Replay — 35 seconds

Click `Restart engine` in the WAL panel.

Watch the before/after digest and `equal: true`, plus restored chains,
hypotheses, and routines. The deviation store is ephemeral and cleared on
restart.

Say: “Durable memory is versioned, verifiable, and replayable.”

## Always state the limitation

`synthetic_episode_not_separated` remains visible. The cognitive mechanics are
functional and qualified; behavioral calibration still needs real homes and
sensors. Security decisions remain under the historical engine’s control.

## Recovery

If the session is unexpected, click `Reset session`, then `Load real baseline ·
7 days`. If a batch is too long, click `Cancel batch`. Use `Technical mode` for
engineering details.
