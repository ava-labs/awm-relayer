# How to Contribute to ICM Off-chain Services

## Setup

To start developing on ICM Off-chain services, you'll need Golang v1.22.8.

## Issues

### Security

- Do not open up a GitHub issue if it relates to a security vulnerability in ICM Off-chain services, and instead refer to our [security policy](./SECURITY.md).

### Making an Issue

- Check that the issue you're filing doesn't already exist by searching under [issues](https://github.com/ava-labs/icm-offchain-services/issues).
- If you're unable to find an open issue addressing the problem, [open a new one](https://github.com/ava-labs/icm-offchain-services/issues/new/choose). Be sure to include a *title and clear description* with as much relevant information as possible.

## Features

- If you want to start a discussion about the development of a new feature or the modification of an existing one, start a thread under GitHub [discussions](https://github.com/ava-labs/icm-offchain-services/discussions/categories/ideas).
- Post a thread about your idea and why it should be added to ICM Offchain Services.
- Don't start working on a pull request until you've received positive feedback from the maintainers.

## Pull Request Guidelines

- Open a new GitHub pull request containing your changes.
- All commits must be [signed](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits).
- Ensure the PR description clearly describes the problem and solution, and how the change was tested. Include the relevant issue number if applicable.
- If your PR isn't ready to be reviewed just yet, you can open it as a draft to collect early feedback on your changes.
- Once the PR is ready for review, mark it as ready-for-review and request review from one of the maintainers.

### Testing

#### Local

- Run the unit tests

```sh
./scripts/test.sh
```

### Continuous Integration (CI)

- Pull requests will generally not be approved or merged unless they pass CI.

## Other

### Do you have questions about the source code?

- Ask any question about ICM Off-chain Services under GitHub [discussions](https://github.com/ava-labs/icm-offchain-services/discussions/categories/q-a).
