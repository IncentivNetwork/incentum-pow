# DPoW Documentation

Documentation for the Delegated Proof-of-Work (DPoW) miner-authorization subsystem — a stake-based mining authorization layer on top of Ethash Proof-of-Work.

| Document | Audience | Read it for |
|---|---|---|
| [DPOW_SPECIFICATION.md](DPOW_SPECIFICATION.md) | engineers | Architecture, smart-contract and Geth-client design, storage layout, formal specification, and the operational & governance model. |
| [DPOW_NODE_OPERATOR_GUIDE.md](DPOW_NODE_OPERATOR_GUIDE.md) | node operators (miners, RPC, archive) | Upgrading a node for the hard fork, the mandatory service-file hardening checklist, and activation-day checks. |
| [DPOW_MINER_SIGNATURE.md](DPOW_MINER_SIGNATURE.md) | node operators, release owners, reviewers | The block signature rule, the signer key on a producer, reacting to a new stake, the rollout, and holding the rule back |
| [DPOW_MINER_ONBOARDING.md](DPOW_MINER_ONBOARDING.md) | miner operators | Acquiring and staking WCENT, dual maturity timing, the staking deadline, and the unstake lifecycle. |
| [DPOW_GOVERNANCE_RUNBOOK.md](DPOW_GOVERNANCE_RUNBOOK.md) | Timelock signers, canceller guardian | Scheduling / executing / cancelling Timelock operations, emergency miner removal, guardian setup, and incident response. |

## Where to start

- New to DPoW — read `DPOW_SPECIFICATION.md` §1–§3.
- Operating a node through the hard fork — `DPOW_NODE_OPERATOR_GUIDE.md`.
- Mining and need to stake — `DPOW_MINER_ONBOARDING.md`.
- Producing blocks past the signer threshold — `DPOW_MINER_SIGNATURE.md`.
- Holding a governance or guardian key — `DPOW_GOVERNANCE_RUNBOOK.md`.

`DPOW_SPECIFICATION.md` is the source of truth for design and behavior; the guides are operational and reference it. `DPOW_MINER_SIGNATURE.md` is the source of truth for the signature rule.
