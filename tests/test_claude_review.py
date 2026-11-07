"""A Claude action success does not prove that its shell tools worked (issue 21)."""
import copy
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


def execution():
    return [
        {'type': 'assistant', 'message': {'content': [
            {'type': 'tool_use', 'id': 'diff', 'name': 'Bash',
             'input': {'command': 'git diff origin/main...pr/head'}}]}},
        {'type': 'user', 'message': {'content': [
            {'type': 'tool_result', 'tool_use_id': 'diff', 'content': 'sample diff'}]}},
        {'type': 'result', 'subtype': 'success', 'is_error': False, 'permission_denials': []},
    ]


class ClaudeReviewTest(unittest.TestCase):
    def check_log(self, messages, conclusion='success'):
        return subprocess.run(
            [sys.executable, '-I', '-B', str(ROOT / 'tools/check_claude_review.py'),
             '-', '--conclusion', conclusion],
            input=json.dumps(messages), text=True, capture_output=True, cwd=ROOT)

    def test_workflow_validates_the_action_log_even_after_failure(self):
        workflow = (ROOT / '.github/workflows/claude-review.yml').read_text()
        self.assertIn('id: review', workflow)
        guard = workflow.split('- name: Validate the Claude review\n')[1]
        self.assertIn('if: ${{ always() }}', guard)
        self.assertIn('steps.review.outputs.execution_file', guard)
        self.assertIn('steps.review.outputs.conclusion', guard)
        self.assertIn('python3 -I -B "$RUNNER_TEMP/check_claude_review.py"', guard)
        self.assertIn('exit 1', guard)

    def test_workflow_keeps_the_validator_before_the_action(self):
        workflow = (ROOT / '.github/workflows/claude-review.yml').read_text()
        before, after = workflow.split('      - uses: anthropics/claude-code-action@')
        self.assertIn('cp tools/check_claude_review.py "$RUNNER_TEMP/check_claude_review.py"', before)
        steps = re.split(r'(?m)^      - ', after)[1:]
        self.assertTrue(steps)
        for step in steps:
            self.assertNotIn('tools/', step)
        self.assertIn('First run exactly git diff origin/main...pr/head as one Bash call.', after)

    def test_validation_records_the_fetched_head(self):
        workflow = (ROOT / '.github/workflows/claude-review.yml').read_text()
        guard = workflow.split('- name: Validate the Claude review\n')[1]
        self.assertIn('head=$(git rev-parse pr/head)', guard)
        self.assertIn('printf \'Reviewed PR head: %s\\n\' "$head" | tee -a "$GITHUB_STEP_SUMMARY"', guard)

    def test_setup_refuses_other_runner_environments_before_system_changes(self):
        for runner in (None, 'self-hosted', 'unknown'):
            env = {'PATH': '/nonexistent'}
            if runner is not None:
                env['RUNNER_ENVIRONMENT'] = runner
            with self.subTest(runner=runner):
                result = subprocess.run(
                    ['/bin/bash', str(ROOT / 'tools/prepare_claude_review.sh')],
                    env=env, text=True, capture_output=True)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stdout, 'Claude review setup requires a GitHub-hosted runner.\n')
                self.assertEqual(result.stderr, '')

    def test_workflow_keeps_isolation_and_tests_the_runner_setup(self):
        workflow = (ROOT / '.github/workflows/claude-review.yml').read_text()
        ci = (ROOT / '.github/workflows/ci.yml').read_text()
        setup = (ROOT / 'tools/prepare_claude_review.sh').read_text()
        for text in (workflow, ci):
            self.assertIn('bash tools/prepare_claude_review.sh', text)
        self.assertIn("CLAUDE_CODE_SUBPROCESS_ENV_SCRUB: '1'", workflow)
        self.assertIn('Read(//proc/**)', workflow)
        self.assertIn('apparmor_restrict_unprivileged_userns=0', setup)
        self.assertIn('bwrap --unshare-user --unshare-pid', setup)
        self.assertIn('--dev /dev --proc /proc', setup)
        self.assertIn('env -i PATH=/usr/bin:/bin', setup)

    def test_required_diff_succeeds(self):
        result = self.check_log(execution())
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('git diff origin/main...pr/head: success', result.stdout)

    def test_empty_diff_output_is_invalid(self):
        for content in ('', ' \n\t', None, [], [{'type': 'text', 'text': ' \n'}],
                        [{'type': 'image', 'data': 'sample'}]):
            log = execution()
            log[1]['message']['content'][0]['content'] = content
            with self.subTest(content=content):
                result = self.check_log(log)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stdout, 'NO VALID CLAUDE REVIEW\n')
        log = execution()
        del log[1]['message']['content'][0]['content']
        self.assertEqual(self.check_log(log).returncode, 1)

    def test_diff_output_can_use_text_blocks(self):
        log = execution()
        log[1]['message']['content'][0]['content'] = [{'type': 'text', 'text': 'sample diff'}]
        self.assertEqual(self.check_log(log).returncode, 0)

    def test_old_diff_command_is_invalid(self):
        log = execution()
        log[0]['message']['content'][0]['input']['command'] = 'git diff HEAD...pr/head'
        self.assertEqual(self.check_log(log).returncode, 1)

    def test_shell_failure_invalidates_an_action_success(self):
        log = execution()
        log[1]['message']['content'][0].update(
            is_error=True, content='bwrap: setting up uid map: Permission denied')
        result = self.check_log(log)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, 'NO VALID CLAUDE REVIEW\n')
        self.assertNotIn('Permission denied', result.stdout)  # Do not print raw tool output.

    def test_a_failed_retry_still_invalidates_the_review(self):
        log = execution()
        attempt = copy.deepcopy(log[:2])
        attempt[0]['message']['content'][0]['id'] = 'retry'
        attempt[0]['message']['content'][0]['input']['command'] = 'git show pr/head:README.md'
        attempt[1]['message']['content'][0].update(tool_use_id='retry', is_error=True)
        result = self.check_log(log[:-1] + attempt + log[-1:])
        self.assertEqual(result.returncode, 1)

    def test_missing_or_failed_evidence_is_not_a_review(self):
        for log in ([], execution()[:1], execution()[1:], execution()[:-1],
                    {'result': 'success'}, 'git diff origin/main...pr/head: success'):
            with self.subTest(log=log):
                self.assertEqual(self.check_log(log).returncode, 1)
        self.assertEqual(self.check_log(execution(), 'failure').returncode, 1)

    def test_text_or_a_different_command_cannot_replace_the_diff(self):
        for command in ('git show pr/head', 'echo "git diff origin/main...pr/head"',
                        'git diff origin/main...pr/head || true', 'git diff origin/main...pr/head --stat'):
            log = execution()
            log[0]['message']['content'][0]['input']['command'] = command
            self.assertEqual(self.check_log(log).returncode, 1)

    def test_permission_denials_and_unsuccessful_results_are_invalid(self):
        for fields in ({'permission_denials': [{'tool_name': 'Bash'}]},
                       {'is_error': True}, {'subtype': 'error_max_turns'}):
            log = execution()
            log[-1].update(fields)
            self.assertEqual(self.check_log(log).returncode, 1)

    def test_unmatched_and_duplicate_results_are_invalid(self):
        log = execution()
        log[1]['message']['content'][0]['tool_use_id'] = 'other'
        self.assertEqual(self.check_log(log).returncode, 1)
        log = execution()
        log.insert(2, copy.deepcopy(log[1]))
        self.assertEqual(self.check_log(log).returncode, 1)

    def test_unreadable_and_malformed_logs_are_invalid(self):
        for source, raw in (('', ''), ('-', '{broken'), ('-', 'null'), ('-', '[null]')):
            result = subprocess.run(
                [sys.executable, '-B', str(ROOT / 'tools/check_claude_review.py'),
                 source, '--conclusion', 'success'], input=raw, text=True, capture_output=True)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stdout, 'NO VALID CLAUDE REVIEW\n')

    def test_non_utf8_log_has_only_a_fixed_failure_message(self):
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / 'execution.json'
            source.write_bytes(b'private log text: \xff\xfe')
            result = subprocess.run(
                [sys.executable, '-B', str(ROOT / 'tools/check_claude_review.py'),
                 str(source), '--conclusion', 'success'], text=True, capture_output=True)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, 'NO VALID CLAUDE REVIEW\n')
        self.assertEqual(result.stderr, '')


if __name__ == '__main__':
    unittest.main()
