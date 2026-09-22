output "load_balancer_dns_name" { value = aws_lb.alexandria.dns_name }
output "object_bucket" { value = aws_s3_bucket.objects.id }
output "database_endpoint" { value = aws_rds_cluster.alexandria.endpoint }
output "batch_job_queue" { value = aws_batch_job_queue.alexandria.arn }
output "batch_job_definition" { value = aws_batch_job_definition.worker.arn }
