
export interface Entity {
    ccaddr: string
    role: string
    host: string
    cdate: string
}

export interface Host {
    fqdn: string
    ccaddr: string
    role: string
    pubkey: string
    cdate: Date
}

