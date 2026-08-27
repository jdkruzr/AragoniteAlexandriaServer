locals {
  tags               = { Application = "Aragonite Loom", ManagedBy = "Terraform" }
  gateway_count      = var.ha ? 2 : 1
  database_min_acu   = var.ha ? 0.5 : 0
  database_instances = var.ha ? 2 : 1
}

resource "random_password" "database" {
  length  = 32
  special = false
}

resource "aws_s3_bucket" "objects" {
  bucket_prefix = "${var.name}-objects-"
  force_destroy = var.object_force_destroy
  tags          = local.tags
}

resource "aws_s3_bucket_versioning" "objects" {
  bucket = aws_s3_bucket.objects.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "objects" {
  bucket = aws_s3_bucket.objects.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "objects" {
  bucket                  = aws_s3_bucket.objects.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_db_subnet_group" "loom" {
  name       = var.name
  subnet_ids = var.private_subnet_ids
  tags       = local.tags
}

resource "aws_security_group" "database" {
  name_prefix = "${var.name}-db-"
  vpc_id      = var.vpc_id
  tags        = local.tags
}

resource "aws_rds_cluster" "loom" {
  cluster_identifier        = var.name
  engine                    = "aurora-postgresql"
  engine_mode               = "provisioned"
  database_name             = "loom"
  master_username           = "loom"
  master_password           = random_password.database.result
  db_subnet_group_name      = aws_db_subnet_group.loom.name
  vpc_security_group_ids    = [aws_security_group.database.id]
  storage_encrypted         = true
  backup_retention_period   = 7
  preferred_backup_window   = "04:00-05:00"
  skip_final_snapshot       = false
  final_snapshot_identifier = "${var.name}-final"
  serverlessv2_scaling_configuration {
    min_capacity             = local.database_min_acu
    max_capacity             = var.aurora_max_acu
    seconds_until_auto_pause = var.ha ? null : 900
  }
  tags = local.tags
}

resource "aws_rds_cluster_instance" "loom" {
  count               = local.database_instances
  identifier          = "${var.name}-${count.index}"
  cluster_identifier  = aws_rds_cluster.loom.id
  instance_class      = "db.serverless"
  engine              = aws_rds_cluster.loom.engine
  engine_version      = aws_rds_cluster.loom.engine_version
  publicly_accessible = false
  tags                = local.tags
}

resource "aws_secretsmanager_secret" "runtime" {
  name_prefix = "${var.name}-runtime-"
  tags        = local.tags
}

resource "aws_secretsmanager_secret_version" "runtime" {
  secret_id = aws_secretsmanager_secret.runtime.id
  secret_string = jsonencode({
    database_url = "postgres://loom:${urlencode(random_password.database.result)}@${aws_rds_cluster.loom.endpoint}:5432/loom?sslmode=require"
  })
}

resource "aws_ecs_cluster" "loom" {
  name = var.name
  setting {
    name  = "containerInsights"
    value = "enhanced"
  }
  tags = local.tags
}

resource "aws_cloudwatch_log_group" "loom" {
  name              = "/ecs/${var.name}"
  retention_in_days = 30
  tags              = local.tags
}

resource "aws_iam_role" "execution" {
  name_prefix        = "${var.name}-exec-"
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Service = ["ecs-tasks.amazonaws.com"] }, Action = "sts:AssumeRole" }] })
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "execution_secret" {
  role   = aws_iam_role.execution.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Action = ["secretsmanager:GetSecretValue"], Resource = aws_secretsmanager_secret.runtime.arn }] })
}

resource "aws_iam_role" "task" {
  name_prefix        = "${var.name}-task-"
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Service = ["ecs-tasks.amazonaws.com"] }, Action = "sts:AssumeRole" }] })
}

resource "aws_iam_role_policy" "task" {
  role = aws_iam_role.task.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      { Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"], Resource = "${aws_s3_bucket.objects.arn}/*" },
      { Effect = "Allow", Action = ["s3:ListBucket"], Resource = aws_s3_bucket.objects.arn },
      { Effect = "Allow", Action = ["batch:SubmitJob"], Resource = "*" }
    ]
  })
}

resource "aws_security_group" "gateway" {
  name_prefix = "${var.name}-gateway-"
  vpc_id      = var.vpc_id
  ingress {
    from_port       = 8443
    to_port         = 8443
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  ingress {
    from_port       = 8089
    to_port         = 8089
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = local.tags
}

resource "aws_security_group_rule" "database_from_gateway" {
  type                     = "ingress"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  security_group_id        = aws_security_group.database.id
  source_security_group_id = aws_security_group.gateway.id
}

resource "aws_security_group" "alb" {
  name_prefix = "${var.name}-alb-"
  vpc_id      = var.vpc_id
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = local.tags
}

resource "aws_lb" "loom" {
  name               = substr(var.name, 0, 32)
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = var.public_subnet_ids
  tags               = local.tags
}

resource "aws_lb_target_group" "main" {
  name_prefix = "loom-m"
  port        = 8443
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"
  health_check {
    path    = "/livez"
    matcher = "200"
  }
}

resource "aws_lb_target_group" "spc" {
  name_prefix = "loom-s"
  port        = 8089
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"
  health_check {
    path    = "/livez"
    matcher = "200"
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.loom.arn
  port              = 80
  protocol          = "HTTP"
  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.loom.arn
  port              = 443
  protocol          = "HTTPS"
  certificate_arn   = var.certificate_arn
  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.main.arn
  }
}

resource "aws_lb_listener_rule" "spc" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 10
  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.spc.arn
  }
  condition {
    host_header {
      values = [var.spc_hostname]
    }
  }
}

resource "aws_ecs_task_definition" "gateway" {
  family                   = "${var.name}-gateway"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.gateway_cpu
  memory                   = var.gateway_memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn
  container_definitions = jsonencode([{
    name                   = "loom", image = var.image_uri, essential = true,
    readonlyRootFilesystem = true,
    portMappings           = [{ containerPort = 8443, protocol = "tcp" }, { containerPort = 8089, protocol = "tcp" }],
    environment = [
      { name = "LOOM_ROLE", value = "gateway" },
      { name = "LOOM_OBJECT_REGION", value = var.region },
      { name = "LOOM_OBJECT_BUCKET", value = aws_s3_bucket.objects.id },
      { name = "LOOM_JOB_LAUNCHER", value = "aws-batch" },
      { name = "LOOM_AWS_BATCH_QUEUE", value = aws_batch_job_queue.loom.arn },
      { name = "LOOM_AWS_BATCH_JOB_DEFINITION", value = aws_batch_job_definition.worker.arn }
    ],
    secrets          = [{ name = "LOOM_DATABASE_URL", valueFrom = "${aws_secretsmanager_secret.runtime.arn}:database_url::" }],
    mountPoints      = [], volumesFrom = [],
    linuxParameters  = { capabilities = { drop = ["ALL"] }, initProcessEnabled = true },
    logConfiguration = { logDriver = "awslogs", options = { awslogs-group = aws_cloudwatch_log_group.loom.name, awslogs-region = var.region, awslogs-stream-prefix = "gateway" } }
  }])
  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "X86_64"
  }
  volume { name = "scratch" }
  tags = local.tags
}

resource "aws_ecs_service" "gateway" {
  name                               = "${var.name}-gateway"
  cluster                            = aws_ecs_cluster.loom.id
  task_definition                    = aws_ecs_task_definition.gateway.arn
  desired_count                      = local.gateway_count
  launch_type                        = "FARGATE"
  health_check_grace_period_seconds  = 120
  deployment_minimum_healthy_percent = var.ha ? 50 : 0
  deployment_maximum_percent         = 200
  network_configuration {
    subnets          = var.private_subnet_ids
    security_groups  = [aws_security_group.gateway.id]
    assign_public_ip = var.assign_public_ip
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.main.arn
    container_name   = "loom"
    container_port   = 8443
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.spc.arn
    container_name   = "loom"
    container_port   = 8089
  }
  depends_on = [aws_lb_listener.https, aws_lb_listener_rule.spc]
  tags       = local.tags
}

resource "aws_batch_compute_environment" "loom" {
  name = var.name
  type = "MANAGED"
  compute_resources {
    type               = "FARGATE"
    max_vcpus          = 32
    subnets            = var.private_subnet_ids
    security_group_ids = [aws_security_group.gateway.id]
  }
  tags = local.tags
}

resource "aws_batch_job_queue" "loom" {
  name     = var.name
  state    = "ENABLED"
  priority = 1
  compute_environment_order {
    order               = 1
    compute_environment = aws_batch_compute_environment.loom.arn
  }
  tags = local.tags
}

resource "aws_batch_job_definition" "worker" {
  name                  = "${var.name}-worker"
  type                  = "container"
  platform_capabilities = ["FARGATE"]
  container_properties = jsonencode({
    image                = var.image_uri,
    command              = [],
    executionRoleArn     = aws_iam_role.execution.arn,
    jobRoleArn           = aws_iam_role.task.arn,
    resourceRequirements = [{ type = "VCPU", value = var.worker_cpu }, { type = "MEMORY", value = var.worker_memory }],
    environment = [
      { name = "LOOM_ROLE", value = "worker" },
      { name = "LOOM_OBJECT_REGION", value = var.region },
      { name = "LOOM_OBJECT_BUCKET", value = aws_s3_bucket.objects.id }
    ],
    secrets                      = [{ name = "LOOM_DATABASE_URL", valueFrom = "${aws_secretsmanager_secret.runtime.arn}:database_url::" }],
    readonlyRootFilesystem       = true,
    networkConfiguration         = { assignPublicIp = var.assign_public_ip ? "ENABLED" : "DISABLED" },
    fargatePlatformConfiguration = { platformVersion = "LATEST" },
    logConfiguration             = { logDriver = "awslogs", options = { awslogs-group = aws_cloudwatch_log_group.loom.name, awslogs-region = var.region, awslogs-stream-prefix = "worker" } }
  })
  retry_strategy { attempts = 1 }
  timeout { attempt_duration_seconds = 3600 }
  tags = local.tags
}
