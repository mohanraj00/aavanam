"""A Claude action success does not prove that its shell tools worked (issue 21)."""
import copy
import json
from pathlib import Path
import subprocess
import sys
import unittest


ROOT = Path(__file__).resolve().parents[1]


def execution():
    return [
        {'type': 'assistant', 'message': {'content': [
            {'type': 'tool_use', 'id': 'diff', 'name': 'Bash',
             'input': {'command': 'git diff HEAD...pr/head'}}]}},
        {'type': 'user', 'message': {'content': [
            {'type': 'tool_result', 'tool_use_id': 'diff', 'content': 'sample diff'}]}},
        {'type': 'result', 'subtype': 'success', 'is_error': False, 'permission_denials': []},
    ]


class ClaudeReviewTest(unittest.TestCase):
    def check_log(self, messages, conclusion='success'):
        return subprocess.run(
            [sys.executable, '-B', str(ROOT / 'tools/check_claude_review.py'),
             '-', '--conclusion', conclusion],
            input=json.dumps(messages), text=True, capture_output=True, cwd=ROOT)

    def test_workflow_validates_the_action_log_even_after_failure(self):
        workflow = (ROOT / '.github/workflows/claude-review.yml').read_text()
        self.assertIn('id: review', workflow)
        guard = workflow.split('- name: Validate the Claude review\n')[1]
        self.assertIn('if: ${{ always() }}', guard)
        self.assertIn('steps.review.outputs.execution_file', guard)
        self.assertIn('steps.review.outputs.conclusion', guard)
        self.assertIn('python3 -B tools/check_claude_review.py', guard)
        self.assertIn('exit 1', guard)

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
        self.assertIn('git diff HEAD...pr/head: success', result.stdout)

    def test_shell_failure_invalidates_an_action_success(self):
        log = execution()
        log[1]['message']['content'][0].update(
            is_error=True, content='bwrap: setting up uid map: Permission denied')
        result = self.check_log(log)
        self.assertEqual(result.returncode, 1)
        self.assertIn('NO VALID CLAUDE REVIEW', result.stdout)
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
                    {'result': 'success'}, 'git diff HEAD...pr/head: success'):
            with self.subTest(log=log):
                self.assertEqual(self.check_log(log).returncode, 1)
        self.assertEqual(self.check_log(execution(), 'failure').returncode, 1)

    def test_text_or_a_different_command_cannot_replace_the_diff(self):
        for command in ('git show pr/head', 'echo "git diff HEAD...pr/head"',
                        'git diff HEAD...pr/head || true', 'git diff HEAD...pr/head --stat'):
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
            self.assertIn('NO VALID CLAUDE REVIEW', result.stdout)


if __name__ == '__main__':
    unittest.main()
