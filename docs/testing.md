# Testing

Draft outline. See also section 5 of
[Use Cases and Direction](use-cases-and-direction.md).

## Overview

Test cases are the first concrete use cases for the lab. Each one has to
bring up a topology, configure it, exercise it and check the result, which
is what a person using the lab does by hand. Anything awkward for a test is
likely awkward for a user too.

TODO: what the tests cover, and how they map to the use cases.

## Approach

Goals:

- An extensible architecture: new tests, topologies and profiles can be
  added without changing the infrastructure.
- Avoid both extremes: a pile of shell scripts that grows by accretion, and
  a framework that is heavy, hard to understand and cumbersome to use.

There are two levels of testing:

- **Static and unit checks:** `go test`, formatting, linters and shell
  checks. No lab is needed, so they run first in CI and locally when code
  changes.
- **Lab tests:** bring up a topology, run some operations and check the
  status or output. `labctl` is the control mechanism for this level.

A test case is a user flow: a sequence of `labctl` steps, possibly mixed
with external operations, written the way a user would run them. The one
difference is that a test always terminates with a status, while a user
flow may be open ended. A test set, such as a CI acceptance run or a larger
regression check, is a set of these flows.

TODO: the format of a flow, and what runs it.

## CI Integration

Manual runs and CI should use the same flow as much as possible, so that a
CI failure can be reproduced locally with the same command.

TODO: how the configuration for a run is specified (which tests, across
which part of the matrix).
