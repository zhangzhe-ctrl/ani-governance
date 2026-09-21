// Use scripts/atlas.sh: Ent's loader must run under app/admin/service because
// the schema lives in a Go internal package.
env "governance" {
  src = "ent://internal/data/ent/schema"
  url = getenv("ANI_DATABASE_DSN")
  dev = getenv("ANI_ATLAS_DEV_DSN")
  migration {
    dir = "file://../../../migrations"
  }
}
