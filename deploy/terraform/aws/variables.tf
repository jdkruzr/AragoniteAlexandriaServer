variable "name" {
  type    = string
  default = "aragonite-alexandria-server"
}

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "database_name" {
  type    = string
  default = "alexandria"
}

variable "database_username" {
  type    = string
  default = "alexandria"
}

variable "main_target_prefix" {
  type    = string
  default = "axa-m"
}

variable "spc_target_prefix" {
  type    = string
  default = "axa-s"
}

variable "image_uri" { type = string }
variable "vpc_id" { type = string }
variable "public_subnet_ids" { type = list(string) }
variable "private_subnet_ids" { type = list(string) }
variable "main_hostname" { type = string }
variable "spc_hostname" { type = string }
variable "certificate_arn" { type = string }

variable "assign_public_ip" {
  type    = bool
  default = false
}

variable "ha" {
  type    = bool
  default = false
}

variable "gateway_cpu" {
  type    = number
  default = 512
}

variable "gateway_memory" {
  type    = number
  default = 1024
}

variable "worker_cpu" {
  type    = string
  default = "2"
}

variable "worker_memory" {
  type    = string
  default = "4096"
}

variable "aurora_max_acu" {
  type    = number
  default = 4
}

variable "object_force_destroy" {
  type    = bool
  default = false
}
