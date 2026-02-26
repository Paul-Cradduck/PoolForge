terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region
}

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

resource "aws_security_group" "poolforge_test" {
  name_prefix = "poolforge-test-${var.run_id}-"
  vpc_id      = var.vpc_id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name    = "poolforge-test-${var.run_id}"
    Project = "poolforge"
    RunID   = var.run_id
  }
}

resource "aws_instance" "test" {
  ami                         = data.aws_ami.ubuntu.id
  instance_type               = var.instance_type
  key_name                    = var.key_name
  vpc_security_group_ids      = [aws_security_group.poolforge_test.id]
  subnet_id                   = var.subnet_id
  associate_public_ip_address = true

  root_block_device {
    volume_size = 20
  }

  tags = {
    Name    = "poolforge-test-${var.run_id}"
    Project = "poolforge"
    RunID   = var.run_id
  }
}

resource "aws_ebs_volume" "disks" {
  count             = length(var.volume_sizes)
  availability_zone = aws_instance.test.availability_zone
  size              = var.volume_sizes[count.index]
  type              = "gp3"

  tags = {
    Name    = "poolforge-test-${var.run_id}-disk${count.index}"
    Project = "poolforge"
    RunID   = var.run_id
  }
}

resource "aws_volume_attachment" "disks" {
  count       = length(var.volume_sizes)
  device_name = "/dev/sd${["f","g","h","i","j","k"][count.index]}"
  volume_id   = aws_ebs_volume.disks[count.index].id
  instance_id = aws_instance.test.id
}
