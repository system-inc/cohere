# Primitive property constraints

React Compiler 1.0.0 infers primitive operands for arithmetic and ordering
operators before computing mutation effects. A primitive property read creates
a primitive value; it does not capture the mutable identity of its receiver.
Cohere previously retained that identity even when later arithmetic established
the primitive constraint. In TimeSeriesChart, this connected yAxisHeadroom to
later configuration mutation and incorrectly invalidated its memoized callback.

The inference here is deliberately bounded, not a replacement for the reference
type solver or an appeal to TypeScript annotations. Ordered constraints follow
local assignments and proven constant captures across function boundaries.
Keys include the owning function because identifier numbers restart in children.
Mutable context cells do not transmit constraints backwards. Known receivers,
computed reads, ref current reads and phi results remain conservative barriers.
A previously known function or object is not overwritten by an arithmetic use.

Only property reads inferred primitive change their aliasing signature. Their
original operands still participate in reactivity and dependency collection.
There is no preservation-validator exemption. Equality and unsigned right shift
do not constrain their operands in the reference's operator table; neither does
this inference. The reference's default does not enable TypeScript annotations.

## Discriminating fixtures

Eight exact sources were compared with compiler 1.0.0, with independent bad
dependency controls proving that the instrument can fail. Arithmetic, two
capture boundaries and removal of the later mutation compile successfully.
Equality, identity-only use and an earlier call of the value each produce two
preservation errors. Missing dependencies and constant inputs each produce one.

Before this repair, the permanent arithmetic and nested-capture fixtures each
report two false positives; constant inputs report two errors instead of one.
Afterwards all eight match the reference. The constant-input control depends on
the independently landed nested-scope correction, rather than being silenced.
Operator and identity tests also inspect the inferred set and resulting effects,
including mutable captures, shadows, unrelated nested identifiers and barriers.

## Corpus attribution

The hoistable corpus changes from 795 deep / 3302 flat dependencies to
795 / 3303. Comparing every emitted dependency identifies exactly one addition:
TimeSeriesChart's yAxisHeadroom root, identifier 530. There are no removals or
path-depth changes. Scope and merge corpus measurements are unchanged. This is
the scalar now surviving as its own dependency rather than remaining attached
to the mutable configuration. The full lint suite and build are the landing gate.

The preceding nested-scope commit a7e77e4 was measured separately on the frozen
snapshot: nine findings, with zero additions and zero removals. Its binary SHA256
is afb32259dc87dd22a92da797c5f137e2fcda14f030b6798439a4c5f3a15abe80.
The arithmetic population must be measured from its own committed binary.
