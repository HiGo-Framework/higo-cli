# test-boilerplate

Run the boilerplate integration tests for higo-cli. These tests generate real projects for every service combination, resolve dependencies, build, and vet each one.

## Steps

1. Run the test suite from the higo-cli root:
   ```
   cd /Users/triasbratayudhana/dev/Hi/higo-cli
   go test ./internal/generator/... -v -run TestBoilerplateBuilds -timeout 5m
   ```

2. Parse the output and report a clear summary table:
   - List each test case (project name) with PASS ✓ or FAIL ✗
   - For any failure, show the exact error output (generate / go get / go mod tidy / go build / go vet)
   - Show total pass/fail counts at the end

3. If any test fails:
   - Read the generated files in the temp directory (printed in the failure output) to diagnose the root cause
   - Identify which template file needs fixing
   - Propose the fix

4. If all tests pass, confirm with the pass count and the combinations that were verified.
