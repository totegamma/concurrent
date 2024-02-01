import { Box, Button, Tab, Tabs } from "@mui/material"
import {  useEffect, useState } from "react"
import { Navigate, useLocation } from "react-router-dom"
import { Entities } from "../widgets/entities"
import { Domains } from "../widgets/domains"
import { useApi } from "../context/apiContext"

export const Home = (): JSX.Element => {

    const [tab, setTab] = useState(0)
    const [tags, setTag] = useState<string[]>([])
    const { api } = useApi()

    const ccid = api.getTokenClaims()?.iss

    useEffect(() => {
        if (!ccid) return
        api.getEntity(ccid).then((entity) => {
            setTag(entity?.tag.split(',') ?? [])
        })
    }, [api])

    if (!api.token) return <Navigate to='/welcome' state={{ from: useLocation() }} replace={true} />
    return (
        <Box
            display='flex'
            flexDirection='column'
            overflow='hidden'
            width='100%'
        >
            <Box
                display='flex'
                flexDirection='column'
                width='100%'
            >
                hello {ccid}<br />
                your tag is {tags.join(',')}<br />

                <Tabs
                    value={tab}
                    onChange={(_, index) => {
                        setTab(index)
                    }}
                >
                    <Tab label='Hello' />
                    {tags.includes("_admin") && <Tab label="Entities" />}
                    {tags.includes("_admin") && <Tab label="Hosts" />}
                </Tabs>
            </Box>

            <Box sx={{
                display: 'flex',
                flex: 1,
                flexDirection: 'column',
                mt: '20px',
                overflowX: 'hidden',
                overflowY: 'auto',
                width: '100%'
            }}>
                {tab === 0 &&
                    <Box sx={{
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 1,
                        width: '100%'
                    }}>
                        まだ未実装の機能たち
                        <Button variant='contained'>招待コードの発行</Button>
                        <Button variant='contained'>アカウントの転出</Button>
                        <Button color='error' variant='contained'>アカウントの凍結</Button>
                    </Box>
                }
                {tab === 1 &&
                    <Entities />
                }
                {tab === 2 &&
                    <Domains />
                }
            </Box>
        </Box>
    )
}
