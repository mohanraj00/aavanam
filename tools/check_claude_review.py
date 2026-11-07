#!/usr/bin/env python3
"""Reject a Claude review without successful shell evidence (issue 21)."""
import argparse
import json
from pathlib import Path
import sys


DIFF = 'git diff HEAD...pr/head'
INVALID = 'NO VALID CLAUDE REVIEW'


def validate(messages, conclusion):
    if conclusion != 'success' or not isinstance(messages, list) or not messages:
        raise ValueError('The action or its execution log is incomplete.')
    calls, results = {}, {}
    for message in messages:
        if not isinstance(message, dict):
            raise ValueError('The execution log has an invalid message.')
        if message.get('type') not in ('assistant', 'user'):
            continue
        content = message.get('message', {}).get('content', [])
        if not isinstance(content, list):
            continue  # A text-only user message has no tool evidence.
        for block in content:
            if not isinstance(block, dict):
                raise ValueError('The execution log has an invalid content block.')
            if block.get('type') == 'tool_use':
                key = block.get('id')
                if not isinstance(key, str) or key in calls:
                    raise ValueError('A tool call has no unique ID.')
                calls[key] = block
            elif block.get('type') == 'tool_result':
                key = block.get('tool_use_id')
                if not isinstance(key, str) or key not in calls or key in results:
                    raise ValueError('A tool result has no unique call.')
                results[key] = block
    final = messages[-1]
    if (final.get('type') != 'result' or final.get('subtype') != 'success'
            or final.get('is_error') is not False or final.get('permission_denials')):
        raise ValueError('Claude did not complete without errors or permission denials.')
    diff_ok = False
    for key, call in calls.items():
        if call.get('name') != 'Bash':
            continue
        result = results.get(key)
        if result is None or result.get('is_error', False) is not False:
            raise ValueError('A shell call failed or has no result.')
        if call.get('input', {}).get('command', '').strip() == DIFF:
            diff_ok = True
    if not diff_ok:
        raise ValueError('The required diff has no successful shell result.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('execution_file', help='action execution_file output, or - for stdin')
    parser.add_argument('--conclusion', required=True)
    args = parser.parse_args()
    try:
        raw = sys.stdin.read() if args.execution_file == '-' else Path(args.execution_file).read_text()
        validate(json.loads(raw), args.conclusion)
    except (OSError, ValueError, TypeError, AttributeError) as error:
        # Do not print the execution log. Tool output can contain sensitive data.
        reason = str(error) if isinstance(error, ValueError) and not isinstance(error, json.JSONDecodeError) else 'The execution log cannot be read.'
        print(INVALID + ': ' + reason)
        return 1
    print(DIFF + ': success')
    print('Claude shell evidence: valid. Read the review findings before approval.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
