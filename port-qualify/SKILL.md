---
name: port-qualify
description: Use when a JVM-to-Rust port is being proposed and, before committing to it, you need to decide in about a day whether the port can pay for itself and whether the codebase is port-shaped at all — measures whether the JVM is actually the cost driver (CPU or memory), checks for the constructs no reference model can express, and issues a go / no-go. Phase 0 of port-jvm-to-rust. Triggers — "should we port this to Rust", "is this worth porting", "qualify the port", "is the JVM the cost here", "port go/no-go", "how much would Rust save".
user_invocable: true
---

# port-qualify

Decide, cheaply and with numbers, whether a JVM-to-Rust port is worth doing and whether it is doable with a behavior-preserving ladder. A large fraction of legacy JVM systems are I/O- or database-bound, where a flawless port saves a few percent; and a codebase whose core depends on reflection, JNI, or the memory model has no oracle a sequential reference model can express. Both are cheaper to learn on day one than after the port.

Output is `.port/qualify.md` with a go / no-go and the two numbers a buyer asks for first. **A no-go is a real deliverable.**

## When to Use

Use when:
- Someone proposes porting Java / Kotlin / JVM code to Rust for cost, performance or memory reasons.
- Before invoking any other `port-*` phase — this is phase 0.
- A buyer asks "how much would we save" and the honest answer needs a measurement, not a multiplier.

Do NOT use when:
- The decision to port is already made for non-economic reasons (safety, ecosystem) — still run the *shape* checks in step 3, skip the economics.
- There is no runnable original — nothing here can be measured.

## Inputs

- The JVM checkout (pinned commit) and a way to run it under a representative workload.
- Access to a production-like profile if one exists (JFR recording, GC logs, instance sizing).

## Workflow

### 1. Establish what "cost" means for this buyer

Ask one question if it is not already answered: is the bill dominated by **CPU-seconds**, **memory (instance size / GB-hours)**, or **tail latency**? Each has a different qualification measurement and a different expected multiple. Record the answer.

### 2. Measure the driver (one day, real workload)

**CPU-bound path:**
- JFR profile under the representative workload. Report on-CPU time *in JVM code* vs waiting (I/O, DB, locks, GC). If the JVM is under ~40% of wall time, a port is unlikely to move the bill; say so.
- JIT-compiled hot loops typically land 1.5–3× off Rust. Do not promise more without a benchmark of the hot path (step 4).

**Memory-bound path (usually the stronger case):**
- `overhead ratio = provisioned heap ÷ irreducible data bytes`. Numerator from the JVM flags; denominator from a JOL histogram of the live set with headers, padding, boxing and reference overhead subtracted.
- Record: object-header size in effect (12–16 B; 8 B with compact object headers if enabled), whether the heap is above the ~32 GB compressed-oops cliff, GC headroom multiple (heap ÷ live set), allocation rate.
- Rule of thumb from the ratio: 6× → a Rust port plausibly lands near 1.3× (a 4–5× instance reduction); 1.5× (already off-heap or byte-array dominated) → the port buys little.
- **Ask whether the cheap Java-side fixes have been tried**: primitive collections (Eclipse Collections, fastutil), off-heap buffers, compact headers, heap sizing below the cliff. If a JOL histogram says 60% of the heap is boxing and headers, a Java-side representation fix may get half the win for a tenth of the cost. Say that. It is how the engagements where the port *is* the answer get earned.

**Tail-latency path:**
- GC pause distribution from GC logs (p99, p99.9, max). Note which collector; ZGC/Shenandoah have narrowed this a lot, so do not oversell. The structural win is at the tail only.

### 3. Check the shape — can a reference model express this?

Grep and read; this is a census, not an opinion. For each item: absent | in configuration only | in the core.

- reflection (`Class.forName`, `Method.invoke`, annotation-driven dispatch), dynamic classloading, bytecode generation
- JNI / `Unsafe` / `VarHandle` tricks / off-heap `ByteBuffer` semantics that leak
- observable dependence on the JMM (`synchronized` visibility, `volatile` ordering, benign races that produce output)
- observable timing (timeouts, keepalives, backpressure) as part of the output
- width of the public surface: count of public entry points, extension points, user-supplied subclasses, callbacks
- transitive non-JDK dependency count and how much observable behavior they own
- test suite: exists / runs / coverage; git history and issue tracker: exist / mineable

**Stop conditions** (no-go for a behavior-preserving ladder; a redesign may still be fine): reflection, JNI or `Unsafe` *in the core*; observable JMM dependence in the core; an open surface (frameworks with unbounded extension points); no runnable original.

### 4. Benchmark the hot path before promising a number

Port the single hottest pure function or loop (an afternoon), benchmark it with JMH vs criterion on the same inputs, and report the measured multiple. This is the number that goes in the proposal. A port that is byte-exact and 8% faster is a failure the buyer needs to be able to abort on early.

### 5. Write `.port/qualify.md`

```
# Qualification — <target> @ <commit>

Cost driver: CPU | memory | tail-latency       (buyer's answer)
Measurement: <the number and how it was taken>
Expected multiple: <measured on the hot path>, <expected instance reduction>
Cheap Java-side alternatives considered: <list, with why not>
Shape census: <table from step 3>
Stop conditions hit: none | <list>
Verdict: GO | NO-GO | GO-WITH-SCOPE (<which modules>)
Receipt: JDK build, flags, workload, profiler, date
```

## Gate

The verdict is backed by a measurement someone else could re-run from the receipt. **Canary:** re-run the profile on a trivially different workload; if the number does not move, the profile was not measuring the workload.

## Common Mistakes

- **Quoting a multiplier from a blog.** The multiple is measured on this code's hot path or it is not quoted.
- **Skipping the Java-side alternatives.** Being the person who says "you don't need a port, you need fastutil" is what buys the next engagement.
- **Treating the shape census as optional.** Reflection in the core discovered at phase 6 wastes the whole ladder.
- **Confusing "no-go for the ladder" with "no-go for Rust".** A redesign is a different project with a different skill (`maximize-verification` with the old system as differential oracle).
