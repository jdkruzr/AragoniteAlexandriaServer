# Terraform addresses changed; existing infrastructure must not be recreated
# merely for branding. Keep deployed physical names in upgrade variables.
moved {
  from = aws_db_subnet_group.loom
  to   = aws_db_subnet_group.alexandria
}
moved {
  from = aws_rds_cluster.loom
  to   = aws_rds_cluster.alexandria
}
moved {
  from = aws_rds_cluster_instance.loom
  to   = aws_rds_cluster_instance.alexandria
}
moved {
  from = aws_ecs_cluster.loom
  to   = aws_ecs_cluster.alexandria
}
moved {
  from = aws_cloudwatch_log_group.loom
  to   = aws_cloudwatch_log_group.alexandria
}
moved {
  from = aws_lb.loom
  to   = aws_lb.alexandria
}
moved {
  from = aws_batch_compute_environment.loom
  to   = aws_batch_compute_environment.alexandria
}
moved {
  from = aws_batch_job_queue.loom
  to   = aws_batch_job_queue.alexandria
}
