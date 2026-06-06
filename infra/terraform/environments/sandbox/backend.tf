terraform {
  # Create bucket, uncomment and run tofu init -migrate-state
  #
  # backend "s3" {
  #   endpoints                   = { s3 = "https://nbg1.s3.com" }
  #   bucket                      = "rootenv-tfstate"
  #   key                         = "dev/terraform.tfstate"
  #   region                      = "nbg1"
  #   skip_credentials_validation = true
  #   skip_region_validation      = true
  #   skip_requesting_account_id  = true
  #   use_path_style              = true
  # }
}