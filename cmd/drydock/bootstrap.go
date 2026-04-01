package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func cmdBootstrap(args []string) {
	if len(args) > 0 && args[0] == "--help" {
		fmt.Fprintln(os.Stderr, `drydock bootstrap — interactive setup for test infrastructure

Walks you through creating the cloud accounts, repos, and permissions
that Drydock needs to test CI/CD workflows end-to-end via the
github-actions backend.

Note: for local-only testing, use the compose backend instead — it
requires only Docker and does not need any cloud infrastructure.

Supported providers: gcp, aws, all`)
		return
	}

	reader := bufio.NewReader(os.Stdin)
	prompt := func(question, defaultVal string) string {
		if defaultVal != "" {
			fmt.Fprintf(os.Stderr, "%s [%s]: ", question, defaultVal)
		} else {
			fmt.Fprintf(os.Stderr, "%s: ", question)
		}
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		if answer == "" {
			return defaultVal
		}
		return answer
	}

	yesno := func(question string) bool {
		answer := prompt(question+" (y/n)", "y")
		return strings.HasPrefix(strings.ToLower(answer), "y")
	}

	var provider string
	if len(args) > 0 {
		provider = args[0]
	} else {
		provider = prompt("Which providers to set up? (gcp, aws, all)", "all")
	}

	// ── Common inputs ───────────────────────────────────────────────────
	githubOrg := prompt("GitHub owner (org or username)", "")
	testRepo := prompt("Test repository name", "ci-testbed")
	bosunRepo := prompt("Bosun repo (reusable workflows)", githubOrg+"/bosun")
	region := prompt("Primary region", "us-central1")

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "⚠  Cost warning: the generated script creates resources that may incur charges:")
	fmt.Fprintln(os.Stderr, "   GCP: Artifact Registry storage (~$0.10/GB/month), Cloud Storage bucket")
	fmt.Fprintln(os.Stderr, "   AWS: ECR storage (~$0.10/GB/month)")
	fmt.Fprintln(os.Stderr, "   GitHub: Actions minutes (free for public repos)")
	fmt.Fprintln(os.Stderr, "   WIF, IAM, OIDC providers, and service accounts are free.")
	fmt.Fprintln(os.Stderr, "")

	var commands []string

	// ── GitHub test repo ────────────────────────────────────────────────
	if yesno("Create GitHub test repo?") {
		commands = append(commands,
			"# ── GitHub Test Repo ─────────────────────────────────────────────",
			fmt.Sprintf("gh repo create %s/%s --public --clone", githubOrg, testRepo),
			fmt.Sprintf("cd %s", testRepo),
			`cat > Dockerfile <<'EOF'
FROM alpine:3.21
RUN echo "drydock test image"
CMD ["echo", "ok"]
EOF`,
			"mkdir -p .github/workflows",
			`git add -A && git commit -m "chore: initial test repo for Drydock"`,
			"git push origin main",
			"cd ..",
			"",
		)
	}

	// ── GCP ─────────────────────────────────────────────────────────────
	if provider == "gcp" || provider == "all" {
		gcpProject := prompt("GCP project ID", "stormbane-test")

		// Discover billing account
		fmt.Fprintln(os.Stderr, "\nLooking up billing accounts...")
		billingAccount := ""
		out, err := exec.Command("gcloud", "billing", "accounts", "list",
			"--format=value(ACCOUNT_ID,DISPLAY_NAME)", "--filter=open=true").Output()
		if err == nil && len(out) > 0 {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) == 1 {
				billingAccount = strings.Fields(lines[0])[0]
				fmt.Fprintf(os.Stderr, "  Found billing account: %s\n", strings.TrimSpace(lines[0]))
			} else {
				fmt.Fprintln(os.Stderr, "  Available billing accounts:")
				for _, l := range lines {
					fmt.Fprintf(os.Stderr, "    %s\n", l)
				}
			}
		}
		billingAccount = prompt("GCP billing account ID", billingAccount)

		wifWorkflowRef := prompt("Reusable workflow ref to restrict WIF to (empty=none)",
			fmt.Sprintf("%s/.github/workflows/docker.yml@refs/heads/main", bosunRepo))

		commands = append(commands,
			"# ── GCP Test Project ─────────────────────────────────────────────",
			fmt.Sprintf("gcloud projects create %s --name='CI/CD Test Sandbox'", gcpProject),
			fmt.Sprintf("gcloud billing projects link %s --billing-account=%s", gcpProject, billingAccount),
			"",
			fmt.Sprintf(`gcloud services enable \
  iam.googleapis.com \
  iamcredentials.googleapis.com \
  artifactregistry.googleapis.com \
  cloudresourcemanager.googleapis.com \
  sts.googleapis.com \
  --project=%s`, gcpProject),
			"",

			"# Terraform state bucket",
			fmt.Sprintf("gcloud storage buckets create gs://%s-tfstate --project=%s --location=%s --uniform-bucket-level-access",
				gcpProject, gcpProject, region),
			"",

			"# WIF pool + provider",
			fmt.Sprintf(`gcloud iam workload-identity-pools create github-actions-test \
  --location=global \
  --display-name='GitHub Actions (test)' \
  --project=%s`, gcpProject),
			"",
		)

		// Build attribute condition
		attrCondition := fmt.Sprintf("attribute.repository==\"%s/%s\"", githubOrg, testRepo)
		if wifWorkflowRef != "" {
			attrCondition += fmt.Sprintf(" && attribute.job_workflow_ref==\"%s\"", wifWorkflowRef)
		}

		commands = append(commands,
			fmt.Sprintf(`gcloud iam workload-identity-pools providers create-oidc github-oidc \
  --location=global \
  --workload-identity-pool=github-actions-test \
  --issuer-uri=https://token.actions.githubusercontent.com \
  --attribute-mapping='google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.repository_owner=assertion.repository_owner,attribute.ref=assertion.ref,attribute.environment=assertion.environment,attribute.job_workflow_ref=assertion.job_workflow_ref,attribute.actor=assertion.actor' \
  --attribute-condition='%s' \
  --project=%s`, attrCondition, gcpProject),
			"",

			"# Artifact Registry",
			fmt.Sprintf(`gcloud artifacts repositories create ci-test \
  --repository-format=docker \
  --location=%s \
  --project=%s`, region, gcpProject),
			"",

			"# Service account for GitHub Actions",
			fmt.Sprintf(`gcloud iam service-accounts create github-actions-test \
  --display-name='GitHub Actions CI/CD Test' \
  --project=%s`, gcpProject),
			"",

			// WIF → SA binding
			fmt.Sprintf(`gcloud iam service-accounts add-iam-policy-binding \
  github-actions-test@%s.iam.gserviceaccount.com \
  --role=roles/iam.workloadIdentityUser \
  --member="principalSet://iam.googleapis.com/projects/$(gcloud projects describe %s --format='value(projectNumber)')/locations/global/workloadIdentityPools/github-actions-test/attribute.repository/%s/%s" \
  --project=%s`, gcpProject, gcpProject, githubOrg, testRepo, gcpProject),
			"",

			// GAR permissions
			fmt.Sprintf(`gcloud artifacts repositories add-iam-policy-binding ci-test \
  --location=%s \
  --member="serviceAccount:github-actions-test@%s.iam.gserviceaccount.com" \
  --role=roles/artifactregistry.writer \
  --project=%s`, region, gcpProject, gcpProject),
			"",

			fmt.Sprintf(`gcloud artifacts repositories add-iam-policy-binding ci-test \
  --location=%s \
  --member="serviceAccount:github-actions-test@%s.iam.gserviceaccount.com" \
  --role=roles/artifactregistry.reader \
  --project=%s`, region, gcpProject, gcpProject),
			"",

			"# Drydock read-only service account (for assertion checks)",
			fmt.Sprintf(`gcloud iam service-accounts create drydock-reader \
  --display-name='Drydock read-only assertions' \
  --project=%s`, gcpProject),
			"",

			fmt.Sprintf(`gcloud projects add-iam-policy-binding %s \
  --member="serviceAccount:drydock-reader@%s.iam.gserviceaccount.com" \
  --role=roles/viewer`, gcpProject, gcpProject),
			"",

			fmt.Sprintf(`gcloud projects add-iam-policy-binding %s \
  --member="serviceAccount:drydock-reader@%s.iam.gserviceaccount.com" \
  --role=roles/artifactregistry.reader`, gcpProject, gcpProject),
			"",

			"# Print outputs needed for test workflows",
			fmt.Sprintf(`echo ""
echo "=== GCP Bootstrap Complete ==="
echo "WIF Provider: $(gcloud iam workload-identity-pools providers describe github-oidc --location=global --workload-identity-pool=github-actions-test --project=%s --format='value(name)')"
echo "Service Account: github-actions-test@%s.iam.gserviceaccount.com"
echo "GAR Repo: %s-docker.pkg.dev/%s/ci-test"`, gcpProject, gcpProject, region, gcpProject),
			"",
		)
	}

	// ── AWS ─────────────────────────────────────────────────────────────
	if provider == "aws" || provider == "all" {
		awsRegion := prompt("AWS region", "us-east-1")

		commands = append(commands,
			"# ── AWS Test Account ─────────────────────────────────────────────",
			"AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)",
			"",

			"# OIDC provider for GitHub Actions",
			`aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1 \
  --client-id-list sts.amazonaws.com`,
			"",

			"# ECR repository",
			fmt.Sprintf(`aws ecr create-repository \
  --repository-name ci-test/test-image \
  --region %s`, awsRegion),
			"",

			// Trust policy
			fmt.Sprintf(`cat > /tmp/drydock-trust-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Federated": "arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/token.actions.githubusercontent.com"
    },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "token.actions.githubusercontent.com:aud": "sts.amazonaws.com"
      },
      "StringLike": {
        "token.actions.githubusercontent.com:sub": "repo:%s/%s:*"
      }
    }
  }]
}
EOF`, githubOrg, testRepo),
			"",

			`aws iam create-role \
  --role-name github-actions-ci-test \
  --assume-role-policy-document file:///tmp/drydock-trust-policy.json`,
			"",

			`aws iam put-role-policy \
  --role-name github-actions-ci-test \
  --policy-name ecr-push \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": [
        "ecr:GetAuthorizationToken",
        "ecr:BatchCheckLayerAvailability",
        "ecr:PutImage",
        "ecr:InitiateLayerUpload",
        "ecr:UploadLayerPart",
        "ecr:CompleteLayerUpload"
      ],
      "Resource": "*"
    }]
  }'`,
			"",

			"rm /tmp/drydock-trust-policy.json",
			"",

			fmt.Sprintf(`echo ""
echo "=== AWS Bootstrap Complete ==="
echo "Role ARN: arn:aws:iam::${AWS_ACCOUNT_ID}:role/github-actions-ci-test"
echo "ECR Repo: ${AWS_ACCOUNT_ID}.dkr.ecr.%s.amazonaws.com/ci-test/test-image"`, awsRegion),
			"",
		)
	}

	// ── Output ──────────────────────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Generated bootstrap script. Review the commands below, then run them.")
	fmt.Fprintln(os.Stderr, "You can pipe to a file: drydock bootstrap > setup.sh")
	fmt.Fprintln(os.Stderr, "")

	fmt.Println("#!/usr/bin/env bash")
	fmt.Println("set -euo pipefail")
	fmt.Println("")
	for _, cmd := range commands {
		fmt.Println(cmd)
	}
}
