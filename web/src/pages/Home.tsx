import { Box, Fade, Tab, Tabs, Typography } from "@mui/material"
import { Entity, Host } from "../model"
import { useEffect, useState } from "react"
import { getHosts, getEntities } from '../util'
import { Navigate, useLocation } from "react-router-dom"

export const Home = (): JSX.Element => {

    const [tab, setTab] = useState(0)
    const entityJson = localStorage.getItem("ENTITY")
    const entity = entityJson ? (JSON.parse(entityJson) as Entity) : null

    const [entities, setEntities] = useState<Entity[]>([])
    const [hosts, setHosts] = useState<Host[]>([])

    useEffect(() => {
        getHosts().then(setHosts)
        getEntities().then(setEntities)
    }, [])

    if (!entity) return <Navigate to='/welcome' state={{ from: useLocation() }} replace={true} />
    return (
        <>
            hello {entity?.ccaddr}<br />
            your role is {entity?.role}

            {entity?.role === '_admin' && (<>
            <Tabs
                value={tab}
                onChange={(_, index) => {
                    setTab(index)
                }}
            >
                <Tab label="Entities" />
                <Tab label="Hosts" />
            </Tabs>

            <Box sx={{ position: 'relative', mt: '20px' }}>
                <Fade in={tab === 0} unmountOnExit>
                    <Box sx={{ position: 'absolute', width: '100%' }}>
                        <Typography>Entities</Typography>
                        <pre>{JSON.stringify(entities, null, 2)}</pre>
                    </Box>
                </Fade>
                <Fade in={tab === 1} unmountOnExit>
                    <Box sx={{ position: 'absolute', width: '100%' }}>
                        <Typography>Hosts</Typography>
                        <pre>{JSON.stringify(hosts, null, 2)}</pre>
                    </Box>
                </Fade>
            </Box>
            </>)}
        </>
    )

}
