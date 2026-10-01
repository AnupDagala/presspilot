terraform {
  required_version = ">= 1.10, < 2.0"
  required_providers {
    google = {
      source  = "hashicorp/google",
      version = "~> 7.0"
    }
  }
}
provider "google" {
  project = var.project_id
  region  = var.region
}
variable "project_id" {
  type = string
}
variable "region" {
  type    = string
  default = "us-central1"
}
variable "api_image" {
  type = string
}
variable "web_image" {
  type = string
}
variable "worker_image" {
  type = string
}
variable "temporal_address" {
  type = string
}
variable "temporal_namespace" {
  type = string
}
variable "notification_channels" {
  type    = list(string)
  default = []
}
resource "google_project_service" "services" {
  for_each           = toset(["run.googleapis.com", "sqladmin.googleapis.com", "secretmanager.googleapis.com", "artifactregistry.googleapis.com", "monitoring.googleapis.com", "logging.googleapis.com", "telemetry.googleapis.com", "cloudtrace.googleapis.com"])
  service            = each.value
  disable_on_destroy = false
}
resource "google_service_account" "api" {
  account_id = "presspilot-api"
}
resource "google_project_iam_member" "trace_writer" {
  for_each = toset(["roles/telemetry.tracesWriter", "roles/serviceusage.serviceUsageConsumer"])
  project  = var.project_id
  role     = each.value
  member   = "serviceAccount:${google_service_account.api.email}"
}
resource "google_service_account" "web" {
  account_id = "presspilot-web"
}
resource "google_service_account" "worker" {
  account_id = "presspilot-worker"
}
resource "google_sql_database_instance" "business" {
  name                = "presspilot-business"
  database_version    = "POSTGRES_18"
  region              = var.region
  deletion_protection = true
  settings {
    tier              = "db-f1-micro"
    edition           = "ENTERPRISE"
    availability_type = "ZONAL"
    disk_type         = "PD_SSD"
    disk_size         = 10
    backup_configuration {
      enabled                        = true
      point_in_time_recovery_enabled = true
    }
    ip_configuration {
      ipv4_enabled = true
    }
  }
  depends_on = [google_project_service.services]
}
resource "google_sql_database" "business" {
  name     = "presspilot"
  instance = google_sql_database_instance.business.name
}
// Secret containers only. Values are supplied separately from protected files;
// no passwords, provider keys, or connection strings enter Terraform state.
resource "google_secret_manager_secret" "secrets" {
  for_each  = toset(["presspilot-database-url", "presspilot-worker-secret", "presspilot-temporal-key"])
  secret_id = each.value
  replication {
    auto {
    }
  }
  depends_on = [google_project_service.services]
}
resource "google_secret_manager_secret_iam_member" "api_secrets" {
  for_each  = google_secret_manager_secret.secrets
  secret_id = each.value.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}
resource "google_secret_manager_secret_iam_member" "worker_secrets" {
  for_each  = toset(["presspilot-worker-secret", "presspilot-temporal-key"])
  secret_id = google_secret_manager_secret.secrets[each.key].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.worker.email}"
}
resource "google_project_iam_member" "sql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.api.email}"
}
resource "google_cloud_run_v2_service" "api" {
  name                = "presspilot-api"
  location            = var.region
  deletion_protection = true
  template {
    service_account                  = google_service_account.api.email
    max_instance_request_concurrency = 20
    // Dispatcher reconciles the PostgreSQL outbox even without HTTP requests.
    scaling {
      min_instance_count = 1
      max_instance_count = 1
    }
    containers {
      image = var.api_image
      resources {
        limits = {
          cpu    = "1",
          memory = "512Mi"
        }
        cpu_idle = false
      }
      env {
        name  = "API_BIND"
        value = "0.0.0.0"
      }
      env {
        name  = "AUTO_MIGRATE"
        value = "false"
      }
      env {
        name  = "SANDBOX_ENABLED"
        value = "false"
      }
      env {
        name  = "COOKIE_SECURE"
        value = "true"
      }
      env {
        name  = "OTEL_MODE"
        value = "gcp"
      }
      env {
        name  = "GCP_PROJECT"
        value = var.project_id
      }
      env {
        name  = "TEMPORAL_ADDRESS"
        value = var.temporal_address
      }
      env {
        name  = "TEMPORAL_NAMESPACE"
        value = var.temporal_namespace
      }
      env {
        name  = "TEMPORAL_TLS"
        value = "true"
      }
      env {
        name  = "MODEL_DAILY_CALL_LIMIT"
        value = "20"
      }
      env {
        name  = "MODEL_DAILY_TOKEN_LIMIT"
        value = "100000"
      }
      env {
        name = "DATABASE_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["presspilot-database-url"].secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "WORKER_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["presspilot-worker-secret"].secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "TEMPORAL_API_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["presspilot-temporal-key"].secret_id
            version = "latest"
          }
        }
      }
      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }
      startup_probe {
        http_get {
          path = "/healthz"
          port = 8080
        }
      }
      liveness_probe {
        http_get {
          path = "/healthz"
          port = 8080
        }
      }
    }
    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [google_sql_database_instance.business.connection_name]
      }
    }
  }
  depends_on = [google_secret_manager_secret_iam_member.api_secrets, google_project_iam_member.sql_client]
}
resource "google_cloud_run_v2_service" "web" {
  name                = "presspilot-web"
  location            = var.region
  deletion_protection = true
  template {
    service_account = google_service_account.web.email
    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }
    containers {
      image = var.web_image
      resources {
        limits = {
          cpu    = "1",
          memory = "256Mi"
        }
      }
      env {
        name  = "BUSINESS_API"
        value = google_cloud_run_v2_service.api.uri
      }
      env {
        name  = "GCP_AUTH"
        value = "true"
      }
      startup_probe {
        http_get {
          path = "/healthz"
          port = 8080
        }
      }
    }
  }
}
resource "google_cloud_run_v2_service_iam_member" "invoke_api" {
  for_each = {
    web    = google_service_account.web.email,
    worker = google_service_account.worker.email
  }
  name     = google_cloud_run_v2_service.api.name
  location = var.region
  role     = "roles/run.invoker"
  member   = "serviceAccount:${each.value}"
}
resource "google_cloud_run_v2_worker_pool" "worker" {
  name     = "presspilot-worker"
  location = var.region
  scaling {
    manual_instance_count = 1
  }
  template {
    service_account = google_service_account.worker.email
    containers {
      image = var.worker_image
      resources {
        limits = {
          cpu    = "1",
          memory = "1Gi"
        }
      }
      env {
        name  = "BUSINESS_API"
        value = google_cloud_run_v2_service.api.uri
      }
      env {
        name  = "GCP_AUTH"
        value = "true"
      }
      env {
        name  = "LIVE_MODEL_ENABLED"
        value = "false"
      }
      env {
        name  = "TEMPORAL_ADDRESS"
        value = var.temporal_address
      }
      env {
        name  = "TEMPORAL_NAMESPACE"
        value = var.temporal_namespace
      }
      env {
        name  = "TEMPORAL_TLS"
        value = "true"
      }
      env {
        name = "WORKER_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["presspilot-worker-secret"].secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "TEMPORAL_API_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["presspilot-temporal-key"].secret_id
            version = "latest"
          }
        }
      }
      liveness_probe {
        http_get {
          path = "/healthz"
          port = 9090
        }
      }
    }
  }
  depends_on = [google_secret_manager_secret_iam_member.worker_secrets]
}
resource "google_logging_metric" "failures" {
  name   = "presspilot_failures"
  filter = "resource.type=\"cloud_run_revision\" AND (severity>=ERROR OR jsonPayload.level=\"ERROR\") AND resource.labels.service_name=\"presspilot-api\""
  metric_descriptor {
    metric_kind = "DELTA"
    value_type  = "INT64"
  }
}
resource "google_monitoring_alert_policy" "failures" {
  display_name          = "PressPilot backend failures"
  combiner              = "OR"
  notification_channels = var.notification_channels
  conditions {
    display_name = "Backend errors over five minutes"
    condition_threshold {
      filter          = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.failures.name}\" AND resource.type=\"cloud_run_revision\""
      comparison      = "COMPARISON_GT"
      threshold_value = 3
      duration        = "300s"
      aggregations {
        alignment_period   = "60s"
        per_series_aligner = "ALIGN_RATE"
      }
    }
  }
}
output "web_url" {
  value = google_cloud_run_v2_service.web.uri
}
output "api_url" {
  value = google_cloud_run_v2_service.api.uri
}
output "sql_connection_name" {
  value = google_sql_database_instance.business.connection_name
}
