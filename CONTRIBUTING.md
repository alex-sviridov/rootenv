## Development setup

We use [pre-commit](https://pre-commit.com/) to run formatting and linting
automatically before each commit. Set it up once after cloning:

```bash
# install the tool (one-time, system-wide)
brew install pre-commit          # macOS
pip install pre-commit           # alternative
pipx install pre-commit          # alternative

# install the git hook in this repo
pre-commit install
```

Now `tofu fmt` and `terraform-docs` run on every commit. To run them manually against all files:

```bash
pre-commit run --all-files
```

If you need to bypass the hooks in an emergency: `git commit --no-verify` (but please don't make this a habit).