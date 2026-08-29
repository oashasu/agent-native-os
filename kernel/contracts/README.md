# Kernel self-test contract fixtures

The schemas here exist ONLY to exercise the microkernel's own admission,
routing, delegation, fencing and adversarial-qualification suites
(`kernel/scripts/test.sh`, `kernel/tests/integration/`).

They are fixtures, not product contracts. Product contracts live in
`../../contracts/`. Do not build product plugins against anything in this
directory.
