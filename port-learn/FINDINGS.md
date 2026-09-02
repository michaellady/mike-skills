# port-learn — findings index across ports

One row per finding. `bin` ∈ REDISCOVERY | NEW RULE | TOOL GAP | TARGET-LOCAL. `distance` for rediscoveries: where the matched rule lived → where it now fires.

The rediscovery rate per port is the family's health metric. It should fall.

| id | target | phase | bin | rule matched / added | distance | PR |
|---|---|---|---|---|---|---|
| — | (seed) | — | — | The family was seeded from 18 findings in `twitter-port-matrix/evidence/findings/` and the defect classes in `verified-java-websocket-port/.claude/HANDOFF.md`; four of those eighteen were rediscoveries of notes written days earlier by the same owner, which is why this phase exists. | `evidence/prior-art/` → the phase steps | — |
