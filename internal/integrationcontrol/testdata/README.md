# Synthetic compatibility fixtures

These files are deterministic local test inputs only. They do not identify an
installed X-Insight release, a real campaign manifest, a real verification
receipt, or an authorization to import or send traffic.

The JSON line in each file is the canonical product artifact used by tests;
the repository file has one terminal newline for text-file hygiene. No signing
key or credential is present. Real artifacts can be generated only later from
an exact accepted X-Insight release and exact campaign inputs.
