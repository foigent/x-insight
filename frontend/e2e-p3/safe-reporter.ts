import type { FullResult, Reporter, TestCase, TestResult, TestStep } from '@playwright/test/reporter';
import { closeSync, ftruncateSync, openSync, writeSync } from 'node:fs';

// Only explicit phases and booleans leave this reporter; never errors or paths.
export default class SafeReporter implements Reporter {
  private passed = 0;
  private failed = 0;
  private fd: number | undefined;
  private receipt = { schema_version: 'xinsight.p3-runner.v1', stage: 'runner_initialization',
    test_started: false, input_completed: false, body_started: false,
    timed_out: false, global_error_reported: false };
  constructor() {
    const path = process.env.XINSIGHT_P3_RUNNER_RECEIPT;
    if (path) this.fd = openSync(path, 'wx', 0o600);
    this.save();
  }
  private save() {
    if (this.fd === undefined) return;
    const body = JSON.stringify(this.receipt) + '\n';
    writeSync(this.fd, body, 0, 'utf8');
    ftruncateSync(this.fd, Buffer.byteLength(body));
  }
  onTestBegin() {
    this.receipt.stage = 'fixture'; this.receipt.test_started = true; this.save();
  }
  onStepBegin(_test: TestCase, _result: TestResult, step: TestStep) {
    if (step.category !== 'test.step') return;
    if (step.title === 'p3_input_receipts') this.receipt.stage = 'input_receipts';
    if (step.title === 'p3_test_body') {
      this.receipt.stage = 'test_body'; this.receipt.body_started = true;
    }
    this.save();
  }
  onStepEnd(_test: TestCase, _result: TestResult, step: TestStep) {
    if (step.category === 'test.step' && step.title === 'p3_input_receipts' && !step.error) {
      this.receipt.input_completed = true; this.save();
    }
  }
  onError() { this.receipt.global_error_reported = true; this.save(); }
  onTestEnd(_test: TestCase, result: TestResult) {
    if (result.status === 'passed') { this.passed++; this.receipt.stage = 'complete'; }
    else this.failed++;
    this.receipt.timed_out ||= result.status === 'timedOut'; this.save();
  }
  onEnd(result: FullResult) {
    this.receipt.timed_out ||= result.status === 'timedout'; this.save();
    if (this.fd !== undefined) { closeSync(this.fd); this.fd = undefined; }
    process.stdout.write(JSON.stringify({ stage: 'p3_browser', status: result.status,
      passed: this.passed, failed: this.failed }) + '\n');
  }
}
