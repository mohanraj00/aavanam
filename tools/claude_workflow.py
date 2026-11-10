"""Aavanam checks on the Claude review workflow."""
import re

from claude_review import workflow_errors as shared_workflow_errors

PERMISSION_STEP = re.compile(
    r'^  gate:\n.*?^      - name: Require repository write permission\n'
    r'((?:^ {8,}[^\n]*\n)+)^      - id: gate\n', re.M | re.S,
)
PERMISSION_BINDINGS = {
    '        id: permission',
    '          GH_TOKEN: ${{ github.token }}',
    '          REPOSITORY: ${{ github.repository }}',
    '          COMMENTER: ${{ github.event.comment.user.login }}',
    '        run: |',
    '          if ! permission="$(gh api "repos/${REPOSITORY}/collaborators/${COMMENTER}/permission" --jq .permission)"; then',
    '            exit 1',
    '          case "$permission" in',
    '            admin|maintain|write) ;;',
    '              exit 1',
}


def workflow_errors(text):
    errors = shared_workflow_errors(text)
    permission = PERMISSION_STEP.search(text.partition('\n  review:\n')[0])
    if (not permission or not PERMISSION_BINDINGS.issubset(permission[1].splitlines())
            or re.search(r'^        (if|continue-on-error):', permission[1], re.M)):
        errors.append('must require repository write permission before the shared gate')
    return errors
