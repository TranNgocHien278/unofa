# Git Commit Convention

All git commits in this project must strictly adhere to the **`{action} ({scope}): {messages}`** format to maintain a transparent change history. This convention requires clearly classifying the action, defining the scope of impact, and directly describing the update.

## Component Details

### 1. `{action}`

The keyword indicating the type of work performed. Standard keywords commonly used include:

- **`feat`**: Add a new feature.

- **`fix`**: Fix a bug.

- **`refactor`**: Rewrite code without changing existing logic or adding new features.

- **`docs`**: Update documentation (e.g., README, inline comments).

- **`chore`**: Maintenance tasks, dependency updates, or system configuration changes.

- **`test`**: Add or modify test cases.

### 2. `{scope}`

The name of the module, directory, or specific area of the project affected by the commit, enclosed in parentheses.

- _Examples:_ `(api)`, `(db)`, `(ci)`, `(ui)`, `(auth)`.

### 3. `{messages}`

A brief, concise summary of the change.

- **Standard rules:** Start with a lowercase letter, use the imperative mood/present tense (e.g., use "add" instead of "added"), and **do not** place a period at the end of the sentence.

## Reference Examples

- `feat (api): implement user authentication endpoint`

- `fix (db): resolve connection pool timeout during high load`

- `chore (ci): update github actions docker build workflow`

- `refactor (auth): optimize token validation logic`

# No Commented-Out Code

To maintain a clean, readable, and professional codebase, **never commit commented-out code**.

- **Rely on Version Control:** Git inherently tracks all file history. If you ever need to restore old or experimental code, you can easily retrieve it from previous commits. There is no need to keep it in the current working tree.
- **Prevent Codebase Clutter:** Dead or commented-out code creates unnecessary noise, causing confusion and making it much harder for other developers to read, review, and maintain your work.
- **Action Required:** Before staging and committing your changes, always review your diffs and permanently delete any code blocks, functions, or variables that have been commented out.
