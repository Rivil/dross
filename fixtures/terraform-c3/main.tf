# c-3 fixture — two planted IaC defects the language-agnostic fallback is blind to.
# See RUN.md for the documented manual run backing acceptance criterion c-3.

# Defect 1 (tflint, quality / error-handling): a declared-but-unused variable.
# tflint's default ruleset flags this as terraform_unused_declarations. The
# agnostic fallback (scc complexity/LOC, jscpd duplication) has no concept of it.
variable "unused" {
  type    = string
  default = "never referenced"
}

# Defect 2 (trivy config, security): a security group open to the world on SSH.
# trivy's misconfiguration scanner flags the 0.0.0.0/0 ingress (AVD-AWS-0107 /
# aws-vpc-no-public-ingress-sgr). gitleaks/scc/jscpd cannot see a misconfiguration.
resource "aws_security_group" "open" {
  name = "open-ssh"

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
