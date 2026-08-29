# Delivery Manifest

Package: **Agent-Native Microkernel v0.10.0 — M0.5 Adversarial Qualification**

Architecture status: **Microkernel Boundary Candidate / M0.5 Architecture Validation**

This delivery is intended for the next independent adversarial review. It does not declare production certification or a permanently closed Kernel boundary.

## Included

- Architecture Constitution, Microkernel/Protocol specs and historical architecture records;
- current v0.10.0 M0.5 qualification report (`docs/13-...`);
- Go reference Kernel + external Unix-socket CLI;
- authenticated client boundary, Host grants and delegation model;
- stateful authority/writer fencing implementation;
- event provenance/security + durable hash-chain journal reference;
- stream/cancellation/failure lifecycle implementation;
- contract schemas, generated Go bindings and compatibility tooling;
- runtime admission, health/dependency reconciliation and resource enforcement;
- adversarial fixture plugins and process-level qualification suite.

## Validation command

```bash
bash scripts/test.sh
```

This command must pass before packaging.

## Important limitations

See README and `docs/13-M0.5-ADVERSARIAL-QUALIFICATION-v0.10.0.md`. In particular, production OS filesystem/network sandboxing, package-signature trust, distributed authority consensus, remote transport hardening and non-Go generated SDKs remain future hardening work.
