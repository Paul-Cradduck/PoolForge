output "instance_ip" {
  value = aws_instance.test.public_ip
}

output "instance_id" {
  value = aws_instance.test.id
}

output "volume_ids" {
  value = aws_ebs_volume.disks[*].id
}

output "volume_devices" {
  value = aws_volume_attachment.disks[*].device_name
}
