variable "region" {
  default = "us-east-1"
}

variable "instance_type" {
  default = "t3.medium"
}

variable "key_name" {
  description = "SSH key pair name"
}

variable "run_id" {
  description = "Unique test run identifier"
  default     = "manual"
}

variable "volume_sizes" {
  description = "EBS volume sizes in GB (mixed sizes for SHR testing)"
  type        = list(number)
  default     = [1, 2, 2, 3, 4, 10]
}
